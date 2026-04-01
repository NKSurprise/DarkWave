package main

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/gordonklaus/portaudio"
)

func showAudioSettings(w fyne.Window, vc *VoiceClient, onSave func(*portaudio.DeviceInfo, *portaudio.DeviceInfo)) {
	portaudio.Initialize()
	defer portaudio.Terminate()

	devices, err := portaudio.Devices()
	if err != nil {
		dialog.ShowError(fmt.Errorf("could not list audio devices: %w", err), w)
		return
	}

	// separate input and output devices
	var inputDevices []*portaudio.DeviceInfo
	var outputDevices []*portaudio.DeviceInfo
	var inputNames []string
	var outputNames []string

	for _, d := range devices {
		if d.MaxInputChannels > 0 && d.DefaultSampleRate > 0 {
			inputDevices = append(inputDevices, d)
			inputNames = append(inputNames, d.Name)
		}
		if d.MaxOutputChannels > 0 && d.DefaultSampleRate > 0 {
			outputDevices = append(outputDevices, d)
			outputNames = append(outputNames, d.Name)
		}
	}

	// find current selection indices
	inputIdx := 0
	outputIdx := 0
	if vc != nil {
		for i, d := range inputDevices {
			if vc.inputDevice != nil && d.Name == vc.inputDevice.Name {
				inputIdx = i
				break
			}
		}
		for i, d := range outputDevices {
			if vc.outputDevice != nil && d.Name == vc.outputDevice.Name {
				outputIdx = i
				break
			}
		}
	}

	inputSelect := widget.NewSelect(inputNames, nil)
	if len(inputNames) > 0 {
		inputSelect.SetSelectedIndex(inputIdx)
	}

	outputSelect := widget.NewSelect(outputNames, nil)
	if len(outputNames) > 0 {
		outputSelect.SetSelectedIndex(outputIdx)
	}

	content := container.NewVBox(
		widget.NewLabelWithStyle("Audio Settings", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewSeparator(),
		widget.NewLabel("Microphone (input):"),
		inputSelect,
		widget.NewLabel("Speaker (output):"),
		outputSelect,
	)

	dialog.ShowCustomConfirm("Audio Settings", "Save", "Cancel", content, func(save bool) {
		if !save {
			return
		}
		var inputDev, outputDev *portaudio.DeviceInfo
		if inputSelect.SelectedIndex() >= 0 && inputSelect.SelectedIndex() < len(inputDevices) {
			inputDev = inputDevices[inputSelect.SelectedIndex()]
		}
		if outputSelect.SelectedIndex() >= 0 && outputSelect.SelectedIndex() < len(outputDevices) {
			outputDev = outputDevices[outputSelect.SelectedIndex()]
		}
		if vc != nil {
			vc.inputDevice = inputDev
			vc.outputDevice = outputDev
		}
		if onSave != nil {
			onSave(inputDev, outputDev)
		}
	}, w)
}
