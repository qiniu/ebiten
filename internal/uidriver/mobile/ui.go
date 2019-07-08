// Copyright 2016 Hajime Hoshi
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// +build android ios

package mobile

import (
	"context"
	"image"
	"sync"
	"time"

	"golang.org/x/mobile/app"
	"golang.org/x/mobile/event/lifecycle"
	"golang.org/x/mobile/event/paint"
	"golang.org/x/mobile/event/size"
	"golang.org/x/mobile/event/touch"
	"golang.org/x/mobile/gl"

	"github.com/hajimehoshi/ebiten/internal/devicescale"
	"github.com/hajimehoshi/ebiten/internal/driver"
	"github.com/hajimehoshi/ebiten/internal/graphicsdriver/opengl"
)

var (
	glContextCh = make(chan gl.Context)

	// renderCh recieves when updating starts.
	renderCh = make(chan struct{})

	// renderEndCh receives when updating finishes.
	renderEndCh = make(chan struct{})

	theUI = &UserInterface{}
)

func init() {
	theUI.input.ui = theUI
}

func Get() *UserInterface {
	return theUI
}

func (u *UserInterface) Render() {
	renderCh <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-renderEndCh
		cancel()
	}()

	if u.graphics.IsGL() {
		if u.glWorker == nil {
			panic("mobile: glWorker must be initialized but not")
		}
		workAvailable := u.glWorker.WorkAvailable()
	loop:
		for {
			select {
			case <-workAvailable:
				u.glWorker.DoWork()
			case <-ctx.Done():
				break loop
			}
		}
		return
	}

	// TODO: Create and run the thread loop like the GLFW driver does.
}

type UserInterface struct {
	width       int
	height      int
	scale       float64
	sizeChanged bool

	// Used for gomobile-build
	fullscreenScale    float64
	fullscreenWidthPx  int
	fullscreenHeightPx int

	graphics driver.Graphics

	input Input

	glWorker gl.Worker

	m sync.RWMutex
}

var (
	deviceScaleVal float64
	deviceScaleM   sync.Mutex
)

func getDeviceScale() float64 {
	deviceScaleM.Lock()
	defer deviceScaleM.Unlock()

	if deviceScaleVal == 0 {
		deviceScaleVal = devicescale.GetAt(0, 0)
	}
	return deviceScaleVal
}

// appMain is the main routine for gomobile-build mode.
func (u *UserInterface) appMain(a app.App) {
	var glctx gl.Context
	touches := map[touch.Sequence]*Touch{}
	for e := range a.Events() {
		switch e := a.Filter(e).(type) {
		case lifecycle.Event:
			switch e.Crosses(lifecycle.StageVisible) {
			case lifecycle.CrossOn:
				glctx, _ = e.DrawContext.(gl.Context)
				// Assume that glctx is always a same instance.
				// Then, only once initializing should be enough.
				if glContextCh != nil {
					glContextCh <- glctx
					glContextCh = nil
				}
				a.Send(paint.Event{})
			case lifecycle.CrossOff:
				glctx = nil
			}
		case size.Event:
			u.setFullscreenImpl(e.WidthPx, e.HeightPx)
		case paint.Event:
			if glctx == nil || e.External {
				continue
			}
			renderCh <- struct{}{}
			<-renderEndCh
			a.Publish()
			a.Send(paint.Event{})
		case touch.Event:
			switch e.Type {
			case touch.TypeBegin, touch.TypeMove:
				s := getDeviceScale()
				x, y := float64(e.X)/s, float64(e.Y)/s
				// TODO: Is it ok to cast from int64 to int here?
				touches[e.Sequence] = &Touch{
					ID: int(e.Sequence),
					X:  int(x),
					Y:  int(y),
				}
			case touch.TypeEnd:
				delete(touches, e.Sequence)
			}
			ts := []*Touch{}
			for _, t := range touches {
				ts = append(ts, t)
			}
			u.input.update(ts)
		}
	}
}

func (u *UserInterface) Run(width, height int, scale float64, title string, context driver.UIContext, graphics driver.Graphics) error {
	go func() {
		if err := u.run(width, height, scale, title, context, graphics, true); err != nil {
			// As mobile apps never ends, Loop can't return. Just panic here.
			panic(err)
		}
	}()
	app.Main(u.appMain)
	return nil
}

func (u *UserInterface) RunWithoutMainLoop(width, height int, scale float64, title string, context driver.UIContext, graphics driver.Graphics) <-chan error {
	ch := make(chan error)
	go func() {
		defer close(ch)
		if err := u.run(width, height, scale, title, context, graphics, false); err != nil {
			ch <- err
		}
	}()
	return ch
}

func (u *UserInterface) run(width, height int, scale float64, title string, context driver.UIContext, graphics driver.Graphics, mainloop bool) error {
	u.m.Lock()
	u.width = width
	u.height = height
	u.scale = scale
	u.sizeChanged = true
	u.graphics = graphics
	u.m.Unlock()
	// title is ignored?

	if graphics.IsGL() {
		var ctx gl.Context
		if mainloop {
			ctx = <-glContextCh
		} else {
			ctx, u.glWorker = gl.NewContext()
		}
		graphics.(*opengl.Driver).SetMobileGLContext(ctx)
	}

	// Force to set the screen size
	u.updateSize(context)
	for {
		if err := u.update(context); err != nil {
			return err
		}
	}
}

func (u *UserInterface) updateSize(context driver.UIContext) {
	width, height := 0, 0
	actualScale := 0.0

	u.m.Lock()
	sizeChanged := u.sizeChanged
	if sizeChanged {
		width = u.width
		height = u.height
		actualScale = u.scaleImpl() * getDeviceScale()
	}
	u.sizeChanged = false
	u.m.Unlock()

	if sizeChanged {
		// Sizing also calls GL functions
		context.SetSize(width, height, actualScale)
	}
}

func (u *UserInterface) ActualScale() float64 {
	u.m.Lock()
	s := u.scaleImpl() * getDeviceScale()
	u.m.Unlock()
	return s
}

func (u *UserInterface) scaleImpl() float64 {
	scale := u.scale
	if u.fullscreenScale != 0 {
		scale = u.fullscreenScale
	}
	return scale
}

func (u *UserInterface) update(context driver.UIContext) error {
render:
	for {
		t := time.NewTimer(500 * time.Millisecond)
		defer t.Stop()

		select {
		case <-renderCh:
			break render
		case <-t.C:
			context.SuspendAudio()
			continue
		}
	}
	context.ResumeAudio()

	defer func() {
		renderEndCh <- struct{}{}
	}()

	if err := context.Update(func() {
		u.updateSize(context)
	}); err != nil {
		return err
	}
	return nil
}

func (u *UserInterface) ScreenSize() (int, int) {
	u.m.Lock()
	w, h := u.width, u.height
	u.m.Unlock()
	return w, h
}

func (u *UserInterface) ScreenSizeInFullscreen() (int, int) {
	// TODO: This function should return fullscreenWidthPx, fullscreenHeightPx,
	// but these values are not initialized until the main loop starts.
	return 0, 0
}

func (u *UserInterface) SetScreenSize(width, height int) {
	u.m.Lock()
	if u.width != width || u.height != height {
		u.width = width
		u.height = height
		u.updateFullscreenScaleIfNeeded()
		u.sizeChanged = true
	}
	u.m.Unlock()
}

func (u *UserInterface) SetScreenScale(scale float64) {
	u.m.Lock()
	if u.scale != scale {
		u.scale = scale
		u.sizeChanged = true
	}
	u.m.Unlock()
}

func (u *UserInterface) ScreenScale() float64 {
	u.m.RLock()
	s := u.scale
	u.m.RUnlock()
	return s
}

func (u *UserInterface) setFullscreenImpl(widthPx, heightPx int) {
	// This implementation is only for gomobile-build so far.
	u.m.Lock()
	u.fullscreenWidthPx = widthPx
	u.fullscreenHeightPx = heightPx
	u.updateFullscreenScaleIfNeeded()
	u.sizeChanged = true
	u.m.Unlock()
}

func (u *UserInterface) updateFullscreenScaleIfNeeded() {
	if u.fullscreenWidthPx == 0 || u.fullscreenHeightPx == 0 {
		return
	}
	w, h := u.width, u.height
	scaleX := float64(u.fullscreenWidthPx) / float64(w)
	scaleY := float64(u.fullscreenHeightPx) / float64(h)
	scale := scaleX
	if scale > scaleY {
		scale = scaleY
	}
	u.fullscreenScale = scale / getDeviceScale()
	u.sizeChanged = true
}

func (u *UserInterface) ScreenPadding() (x0, y0, x1, y1 float64) {
	u.m.Lock()
	x0, y0, x1, y1 = u.screenPaddingImpl()
	u.m.Unlock()
	return
}

func (u *UserInterface) screenPaddingImpl() (x0, y0, x1, y1 float64) {
	if u.fullscreenScale == 0 {
		return 0, 0, 0, 0
	}
	s := u.fullscreenScale * getDeviceScale()
	ox := (float64(u.fullscreenWidthPx) - float64(u.width)*s) / 2
	oy := (float64(u.fullscreenHeightPx) - float64(u.height)*s) / 2
	return ox, oy, ox, oy
}

func (u *UserInterface) adjustPosition(x, y int) (int, int) {
	u.m.Lock()
	ox, oy, _, _ := u.screenPaddingImpl()
	s := u.scaleImpl()
	as := s * getDeviceScale()
	u.m.Unlock()
	return int(float64(x)/s - ox/as), int(float64(y)/s - oy/as)
}

func (u *UserInterface) IsCursorVisible() bool {
	return false
}

func (u *UserInterface) SetCursorVisible(visible bool) {
	// Do nothing
}

func (u *UserInterface) IsFullscreen() bool {
	return false
}

func (u *UserInterface) SetFullscreen(fullscreen bool) {
	// Do nothing
}

func (u *UserInterface) IsRunnableInBackground() bool {
	return false
}

func (u *UserInterface) SetRunnableInBackground(runnableInBackground bool) {
	// Do nothing
}

func (u *UserInterface) SetWindowTitle(title string) {
	// Do nothing
}

func (u *UserInterface) SetWindowIcon(iconImages []image.Image) {
	// Do nothing
}

func (u *UserInterface) IsWindowDecorated() bool {
	return false
}

func (u *UserInterface) SetWindowDecorated(decorated bool) {
	// Do nothing
}

func (u *UserInterface) IsWindowResizable() bool {
	return false
}

func (u *UserInterface) SetWindowResizable(decorated bool) {
	// Do nothing
}

func (u *UserInterface) IsVsyncEnabled() bool {
	return true
}

func (u *UserInterface) SetVsyncEnabled(enabled bool) {
	// Do nothing
}

func (u *UserInterface) DeviceScaleFactor() float64 {
	return getDeviceScale()
}

func (u *UserInterface) Input() driver.Input {
	return &u.input
}

type Touch struct {
	ID int
	X  int
	Y  int
}

func (u *UserInterface) UpdateInput(touches []*Touch) {
	u.input.update(touches)
}
