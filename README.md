# Diva Remote

Diva is a self-hosted, browser-to-Windows remote desktop prototype. Video and
input travel directly over WebRTC; the small server only serves the browser UI
and exchanges SDP/ICE messages. There is no vendor cloud, account system, TURN,
or media relay. This deliberate design is intended for a machine with a direct
public/white IP.

> This is an MVP, not a security-audited or feature-complete Parsec replacement.
> Do not expose plain HTTP or an unpatched Windows machine to the Internet.

## What works

* Low-latency H.264 desktop video through WebRTC (resolution follows the Windows
  virtual desktop, including multiple monitors).
* Browser keyboard, mouse, wheel, fullscreen, and reconnect status.
* One active viewer per agent, shared-secret authentication, no database.
* An immutable, unprivileged Docker image for the web/signalling service.

Audio, clipboard, file transfer, controller forwarding, unattended service
installation, TURN/NAT traversal, monitor switching, and adaptive bitrate are
not implemented yet. The architecture leaves these as data-channel/media-track
extensions.

## Deploy the server

Create `.env` (use at least 32 random characters):

```dotenv
DIVA_TOKEN=replace-with-output-of-openssl-rand-hex-32
```

Then run `docker compose up -d --build`. Put Caddy, nginx, or another TLS reverse
proxy in front of port 8080. Forward WebSocket upgrades for `/ws`. Open only TCP
443; media uses dynamically selected UDP ports directly between Windows and the
browser, so permit inbound UDP to the Windows host and configure the browser to
reach its public candidate. There are intentionally no public STUN servers.

## Build and run the Windows agent

Install Go 1.23 and FFmpeg (with `libx264`), then:

```powershell
go build -o diva-agent.exe ./cmd/agent
.\diva-agent.exe -server wss://remote.example.com/ws -room gaming-pc `
  -token YOUR_SECRET -fps 60 -bitrate 12000 -ffmpeg C:\ffmpeg\bin\ffmpeg.exe
```

For NVIDIA, lower CPU usage and latency further by replacing `libx264` with
`h264_nvenc` in `cmd/agent/main_windows.go`; Intel and AMD equivalents are
`h264_qsv` and `h264_amf`. Encoder availability depends on the FFmpeg build.
Visit the HTTPS server, enter the same room and token, and connect.

## Production roadmap

For Parsec-class results, replace GDI capture with a native Windows Desktop
Duplication capture module, select NVENC/AMF/Quick Sync dynamically, add bitrate
feedback and frame pacing, use relative/raw mouse input, add Opus audio, and
offer a self-hosted coturn fallback. Code signing, privilege separation,
per-device keys, rate limits, and an external TLS proxy are required before a
public production launch.
