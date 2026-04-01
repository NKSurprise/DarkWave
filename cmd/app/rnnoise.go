package main

/*
#cgo pkg-config: rnnoise
#include <rnnoise.h>
#include <stdlib.h>

DenoiseState* rnnoise_create_wrapper() {
    return rnnoise_create(NULL);
}
*/
import "C"
import "unsafe"

type Denoiser struct {
	state *C.DenoiseState
}

func NewDenoiser() *Denoiser {
	return &Denoiser{state: C.rnnoise_create_wrapper()}
}

func (d *Denoiser) Process(buf []float32) float32 {
	if len(buf) != 480 {
		return 0
	}
	ptr := (*C.float)(unsafe.Pointer(&buf[0]))
	vad := C.rnnoise_process_frame(d.state, ptr, ptr)
	return float32(vad)
}

func (d *Denoiser) Destroy() {
	C.rnnoise_destroy(d.state)
}
