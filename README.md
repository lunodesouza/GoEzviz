# GoEzviz

<p align="center">
  <img src="assets/icon.png" alt="GoEzviz" width="160">
</p>

<p align="center">
  <a href="#lang"><img src="https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white" alt="Go 1.25+"></a>
  <a href="#build"><img src="https://img.shields.io/badge/platform-Windows%20%7C%20Linux%20%7C%20macOS-lightgrey" alt="Windows, Linux, macOS"></a>
  <a href="#features"><img src="https://img.shields.io/badge/protocol-ONVIF%20%2F%20RTSP-2ea44f" alt="ONVIF / RTSP"></a>
</p>

Lightweight IP camera viewer written in Go. Talks to cameras over ONVIF and RTSP — no cloud, no account.

**Homologated camera:** [EZVIZ H9c](https://www.ezviz.com/) (both lenses — main and secondary). Other ONVIF cameras may work, but only the H9c has been validated.

![GoEzviz](docs/Image%20Sep%2010,%202026,%2010_16_00%20AM.png)

## Features

- Automatic ONVIF discovery on the LAN
- Multiple cameras and profiles in a grid; double-click to maximize
- Camera audio, volume control, and PTZ (arrow keys)
- **Talk**: sends the PC microphone to the camera speaker (AAC backchannel)
- Microphone listing on Windows (DirectShow) and macOS (AVFoundation)
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

For a Dock and Finder icon, package the app on a Mac (`assets/icon.icns` is already in the repo):

```bash
./scripts/package-macos.sh
open GoEzviz.app
```

After changing `assets/icon.png`, regenerate `icon.icns` with `./scripts/make-icns.sh` and commit it.

## Usage

Open the app, click **Devices**, use **Discover LAN** or add an IP, enter the camera username and password, refresh ONVIF profiles, and select the ones you want. Then click **Connect**.

On first **Talk** on macOS, the system may ask for microphone access. If it is denied, GoEzviz opens **System Settings → Privacy & Security → Microphone**.

## Contributing

Want to add support for more cameras or brands? Feel free to collaborate — fork the repo and open a pull request.

## License

MIT
