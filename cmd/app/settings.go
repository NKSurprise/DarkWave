package main

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/gordonklaus/portaudio"
)

func showAudioSettings(w fyne.Window, vc *VoiceClient, onSave func(*portaudio.DeviceInfo, *portaudio.DeviceInfo)) {
	// Only initialize PortAudio here when NOT in a voice session.
	// JoinChannel already called Initialize() for the lifetime of the call, and
	// calling Terminate() while streams are running kills them.
	needsInit := vc == nil || !vc.isConnected
	if needsInit {
		portaudio.Initialize()
		defer portaudio.Terminate()
	}

	devices, err := portaudio.Devices()
	if err != nil {
		dialog.ShowError(fmt.Errorf("could not list audio devices: %w", err), w)
		return
	}

	// separate input and output devices
	// Prefer WASAPI-only on Windows: eliminates MME duplicates, truncated names, and
	// virtual mapper devices ("Microsoft Sound Mapper", "Primary Sound Driver").
	// Falls back to all real devices (deduplicated by name) when WASAPI isn't available.
	var inputDevices []*portaudio.DeviceInfo
	var outputDevices []*portaudio.DeviceInfo
	var inputNames []string
	var outputNames []string

	wasapiInputs, wasapiOutputs := filterWASAPI(devices)
	dsInputs, dsOutputs := filterDirectSound(devices)
	if len(dsInputs) > 0 || len(dsOutputs) > 0 {
		// DirectSound: full device names, Windows shared-mode mixer — best quality
		inputDevices = dsInputs
		outputDevices = dsOutputs
	} else if len(wasapiInputs) > 0 || len(wasapiOutputs) > 0 {
		// WASAPI fallback (e.g. DirectSound not installed)
		inputDevices = wasapiInputs
		outputDevices = wasapiOutputs
	} else {
		// non-Windows fallback: deduplicate by name, skip virtual mapper entries
		seen := map[string]bool{}
		for _, d := range devices {
			if isVirtualDevice(d.Name) {
				continue
			}
			if d.MaxInputChannels > 0 && d.DefaultSampleRate > 0 && !seen["in:"+d.Name] {
				seen["in:"+d.Name] = true
				inputDevices = append(inputDevices, d)
			}
			if d.MaxOutputChannels > 0 && d.DefaultSampleRate > 0 && !seen["out:"+d.Name] {
				seen["out:"+d.Name] = true
				outputDevices = append(outputDevices, d)
			}
		}
	}
	for _, d := range inputDevices {
		inputNames = append(inputNames, d.Name)
	}
	for _, d := range outputDevices {
		outputNames = append(outputNames, d.Name)
	}

	// find current selection indices using prefix matching (handles truncated device names)
	// Load from preferences if vc is nil (not in call) or use vc values
	savedInputName := currentApp.Preferences().String("audio.inputDeviceName")
	savedOutputName := currentApp.Preferences().String("audio.outputDeviceName")

	inputName := savedInputName
	outputName := savedOutputName
	if vc != nil {
		if vc.inputDeviceName != "" {
			inputName = vc.inputDeviceName
		}
		if vc.outputDeviceName != "" {
			outputName = vc.outputDeviceName
		}
	}

	inputIdx := 0
	outputIdx := 0
	for i, d := range inputDevices {
		if inputName != "" && (d.Name == inputName ||
			strings.HasPrefix(inputName, d.Name) || strings.HasPrefix(d.Name, inputName)) {
			inputIdx = i
			break
		}
	}
	for i, d := range outputDevices {
		if outputName != "" && (d.Name == outputName ||
			strings.HasPrefix(outputName, d.Name) || strings.HasPrefix(d.Name, outputName)) {
			outputIdx = i
			break
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
		var inputName, outputName string
		if inputSelect.SelectedIndex() >= 0 && inputSelect.SelectedIndex() < len(inputDevices) {
			inputName = inputDevices[inputSelect.SelectedIndex()].Name
		}
		if outputSelect.SelectedIndex() >= 0 && outputSelect.SelectedIndex() < len(outputDevices) {
			outputName = outputDevices[outputSelect.SelectedIndex()].Name
		}

		// Save to preferences so they persist between app sessions
		currentApp.Preferences().SetString("audio.inputDeviceName", inputName)
		currentApp.Preferences().SetString("audio.outputDeviceName", outputName)

		if vc != nil {
			vc.inputDeviceName = inputName
			vc.outputDeviceName = outputName
			if vc.isConnected {
				vc.RestartAudio()
				vc.RestartPlayback()
			}
		}
		if onSave != nil {
			// Get the actual device pointers for onSave callback (for compatibility)
			var inputDev, outputDev *portaudio.DeviceInfo
			if inputName != "" {
				for _, d := range inputDevices {
					if d.Name == inputName {
						inputDev = d
						break
					}
				}
			}
			if outputName != "" {
				for _, d := range outputDevices {
					if d.Name == outputName {
						outputDev = d
						break
					}
				}
			}
			onSave(inputDev, outputDev)
		}
	}, w)
}

// filterWASAPI returns input and output devices that belong to the WASAPI host API.
func filterWASAPI(devices []*portaudio.DeviceInfo) (inputs, outputs []*portaudio.DeviceInfo) {
	for _, d := range devices {
		if d.HostApi == nil || d.HostApi.Type != portaudio.WASAPI {
			continue
		}
		if d.MaxInputChannels > 0 && d.DefaultSampleRate > 0 {
			inputs = append(inputs, d)
		}
		if d.MaxOutputChannels > 0 && d.DefaultSampleRate > 0 {
			outputs = append(outputs, d)
		}
	}
	return
}

// filterDirectSound returns devices belonging to the DirectSound host API.
// DirectSound uses the Windows shared-mode audio mixer (same as MME), gives full
// device names (unlike MME's 31-char truncation), and has no WASAPI exclusive-mode issues.
func filterDirectSound(devices []*portaudio.DeviceInfo) (inputs, outputs []*portaudio.DeviceInfo) {
	for _, d := range devices {
		if d.HostApi == nil || d.HostApi.Type != portaudio.DirectSound {
			continue
		}
		if isVirtualDevice(d.Name) {
			continue
		}
		if d.MaxInputChannels > 0 && d.DefaultSampleRate > 0 {
			inputs = append(inputs, d)
		}
		if d.MaxOutputChannels > 0 && d.DefaultSampleRate > 0 {
			outputs = append(outputs, d)
		}
	}
	return
}

// isVirtualDevice returns true for Windows MME virtual mapper entries that are
// not real hardware (e.g. "Microsoft Sound Mapper", "Primary Sound Driver").
func isVirtualDevice(name string) bool {
	virtual := []string{
		"Microsoft Sound Mapper",
		"Primary Sound",
	}
	for _, v := range virtual {
		if len(name) >= len(v) && name[:len(v)] == v {
			return true
		}
	}
	return false
}
