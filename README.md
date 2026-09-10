# GoEzviz

Lightweight IP camera viewer written in Go. Talks to cameras over ONVIF and RTSP — no cloud, no account.

**Homologated camera:** [EZVIZ H9c](https://www.ezviz.com/) (both lenses — main and secondary). Other ONVIF cameras may work, but only the H9c has been validated.

![GoEzviz](docs/Image%20Sep%2010,%202026,%2010_16_00%20AM.png)

## Features

- Automatic ONVIF discovery on the LAN
- Multiple cameras and profiles in a grid; double-click to maximize
- Camera audio, volume control, and PTZ (arrow keys)
- **Talk**: sends the PC microphone to the camera speaker (AAC backchannel); microphone listing works on Windows (DirectShow) and macOS (AVFoundation)
- Auto-reconnect and saved settings; passwords protected with DPAPI (Windows) or AES-256 (Linux/macOS)

## Requirements

- [Go 1.25+](https://go.dev/dl/)
- Full FFmpeg package (`ffmpeg` and `ffplay` on `PATH`)
- A C compiler (required by Fyne)

## Build

**Windows** (C compiler: [TDM-GCC](https://jmeubank.github.io/tdm-gcc/) or MinGW)

```powershell
go build -ldflags "-H=windowsgui -s -w" -o GoEzviz.exe .
```

**Linux**

```bash
sudo apt install golang gcc libgl1-mesa-dev xorg-dev ffmpeg
go build -ldflags "-s -w" -o GoEzviz .
```

**macOS**

```bash
xcode-select --install
brew install go ffmpeg
go build -ldflags "-s -w" -o GoEzviz .
```

On first Talk use, macOS may ask for microphone permission. Use **Allow mic** in the toolbar to open **System Settings → Privacy & Security → Microphone**, or allow access for GoEzviz (or Terminal when using `go run`).

## Usage

Open the app, click **Devices**, use **Discover LAN** or add an IP, enter the camera username and password, refresh ONVIF profiles, and select the ones you want. Then click **Connect**.

## Contributing

Want to add support for more cameras or brands? Feel free to collaborate — fork the repo and open a pull request.

## License

MIT
