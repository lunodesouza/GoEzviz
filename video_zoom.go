package main

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

var viewZoomLevels = []float32{1, 1.5, 2, 3, 4}

type videoZoom struct {
	widget.BaseWidget
	image  *canvas.Image
	factor float32
}

func newVideoZoom(image *canvas.Image) *videoZoom {
	z := &videoZoom{image: image, factor: 1}
	z.ExtendBaseWidget(z)
	return z
}

func (z *videoZoom) step(delta int) float32 {
	z.factor = stepZoomFactor(z.factor, delta)
	z.Refresh()
	return z.factor
}

func (z *videoZoom) MinSize() fyne.Size {
	z.ExtendBaseWidget(z)
	if z.image != nil {
		return z.image.MinSize()
	}
	return fyne.NewSize(200, 112)
}

func (z *videoZoom) CreateRenderer() fyne.WidgetRenderer {
	return &videoZoomRenderer{zoom: z, objects: []fyne.CanvasObject{z.image}}
}

type videoZoomRenderer struct {
	zoom    *videoZoom
	objects []fyne.CanvasObject
}

func (r *videoZoomRenderer) Layout(size fyne.Size) {
	factor := r.zoom.factor
	if factor < 1 {
		factor = 1
	}
	width := size.Width * factor
	height := size.Height * factor
	r.zoom.image.Resize(fyne.NewSize(width, height))
	r.zoom.image.Move(fyne.NewPos((size.Width-width)/2, (size.Height-height)/2))
}

func (r *videoZoomRenderer) MinSize() fyne.Size {
	return r.zoom.image.MinSize()
}

func (r *videoZoomRenderer) Refresh() {
	r.Layout(r.zoom.Size())
	canvas.Refresh(r.zoom)
}

func (r *videoZoomRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}

func (r *videoZoomRenderer) Destroy() {}

func stepZoomFactor(current float32, delta int) float32 {
	index := zoomLevelIndex(current) + delta
	if index < 0 {
		index = 0
	}
	if index >= len(viewZoomLevels) {
		index = len(viewZoomLevels) - 1
	}
	return viewZoomLevels[index]
}

func zoomLevelIndex(factor float32) int {
	best := 0
	bestDelta := absFloat32(factor - viewZoomLevels[0])
	for i, level := range viewZoomLevels[1:] {
		delta := absFloat32(factor - level)
		if delta < bestDelta {
			best = i + 1
			bestDelta = delta
		}
	}
	return best
}

func absFloat32(value float32) float32 {
	if value < 0 {
		return -value
	}
	return value
}

func formatViewZoom(factor float32) string {
	if factor == float32(int(factor)) {
		return fmt.Sprintf("%d", int(factor))
	}
	return fmt.Sprintf("%.1f", factor)
}
