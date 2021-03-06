// Copyright 2020 The Ebiten Authors
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

// +build js

package monogame

import (
	"github.com/hajimehoshi/ebiten/internal/driver"
	"github.com/hajimehoshi/ebiten/internal/monogame"
)

type Image struct {
	v      *monogame.RenderTarget2D
	width  int
	height int
}

func (i *Image) Dispose() {
	i.v.Dispose()
}

func (*Image) IsInvalidated() bool {
	return false
}

func (*Image) Pixels() ([]byte, error) {
	return nil, nil
}

func (*Image) SetAsDestination() {
}

func (*Image) SetAsSource() {
}

func (i *Image) ReplacePixels(args []*driver.ReplacePixelsArgs) {
	i.v.ReplacePixels(args)
}
