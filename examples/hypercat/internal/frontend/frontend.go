// Package frontend connects HyperCat's terminal model to Ebitengine.
// The model receives Input values and supplies a Presentation; GPU images,
// window callbacks, and platform input polling stay inside this package.
package frontend

import (
	"errors"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/ironpark/gostty/examples/hypercat/ui"
)

// ErrClosed ends the window loop normally, for example after its last tab exits.
var ErrClosed = errors.New("window closed")

// Application is the model driven by the window. All callbacks are serialized
// by Ebitengine. Present must return saved data without reading native handles.
type Application interface {
	Update(Input) error
	Resize(width, height, scale float64)
	Present() Presentation
}

type Config struct {
	Title         string
	Width, Height int // device-independent pixels
}

// Presentation describes one complete window. Slice data is borrowed until the
// next Update; Damage is the only terminal state the renderer mutates.
type Presentation struct {
	Renderer      *Renderer
	Frame         Frame
	Panels        *ui.Panels
	Settings      ui.SettingsValues
	TabBar        *ui.TabBar
	Active, Count int
	TabTitle      func(int) string
	Title         string
	LinkPointer   bool
}

// Run owns the window and its composition surface. The caller owns terminals
// and their per-tab renderers, and closes them after Run returns.
func Run(app Application, config Config) error {
	w := &window{app: app, title: config.Title}
	defer func() {
		if w.content != nil {
			w.content.Deallocate()
		}
	}()
	ebiten.SetWindowSize(config.Width, config.Height)
	ebiten.SetWindowTitle(config.Title)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	ebiten.SetScreenClearedEveryFrame(false)
	err := ebiten.RunGame(w)
	if errors.Is(err, ebiten.Termination) {
		return nil
	}
	return err
}

type window struct {
	app                  Application
	input                inputReader
	content              *ebiten.Image
	title                string
	linkPointer          bool
	width, height, scale float64
}

var _ ebiten.Game = (*window)(nil)
var _ ebiten.LayoutFer = (*window)(nil)

func (w *window) Update() error {
	if err := w.app.Update(w.input.read()); err != nil {
		if errors.Is(err, ErrClosed) {
			return ebiten.Termination
		}
		return err
	}
	return nil
}

func (w *window) Draw(screen *ebiten.Image) {
	p := w.app.Present()
	if p.Renderer == nil {
		return
	}
	if p.Title != w.title {
		w.title = p.Title
		ebiten.SetWindowTitle(p.Title)
	}
	if p.LinkPointer != w.linkPointer {
		w.linkPointer = p.LinkPointer
		shape := ebiten.CursorShapeDefault
		if p.LinkPointer {
			shape = ebiten.CursorShapePointer
		}
		ebiten.SetCursorShape(shape)
	}
	barHeight := float64(int(ui.TabBarHeight * w.scale))
	width, height := screen.Bounds().Dx(), max(screen.Bounds().Dy()-int(barHeight), 1)
	if w.content == nil || w.content.Bounds().Dx() != width || w.content.Bounds().Dy() != height {
		if w.content != nil {
			w.content.Deallocate()
		}
		w.content = ebiten.NewImage(width, height)
	}
	p.Renderer.draw(w.content, p.Frame)
	canvas := p.Renderer.canvas(w.content)
	if p.Panels != nil {
		p.Panels.Draw(canvas, p.Settings)
	}
	screen.Fill(p.Frame.Theme.Panel)
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(0, barHeight)
	screen.DrawImage(w.content, op)
	if p.TabBar != nil {
		canvas.Screen, canvas.Width, canvas.Height = screen, w.width, barHeight
		p.TabBar.Draw(canvas, p.Active, p.Count, p.TabTitle)
	}
}

func (w *window) LayoutF(width, height float64) (float64, float64) {
	w.scale = DeviceScale()
	w.width, w.height = width*w.scale, height*w.scale
	w.app.Resize(w.width, w.height, w.scale)
	return w.width, w.height
}

func (w *window) Layout(width, height int) (int, int) {
	wf, hf := w.LayoutF(float64(width), float64(height))
	return int(wf), int(hf)
}

// DeviceScale returns device pixels per logical pixel, or one before a monitor
// is available. LayoutF supplies the current scale on later window resizes.
func DeviceScale() float64 {
	monitor := ebiten.Monitor()
	if monitor == nil {
		return 1
	}
	if scale := monitor.DeviceScaleFactor(); scale > 0 {
		return scale
	}
	return 1
}
