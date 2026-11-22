# Installing uplet

Requires macOS 15 or later on Apple Silicon.

## Homebrew

```sh
brew tap ernilambar/tap
brew trust ernilambar/tap
brew install ernilambar/tap/uplet
uplet --version
```

## From a release

```sh
curl -fL -o uplet https://github.com/ernilambar/uplet/releases/latest/download/uplet-darwin-arm64
sudo xattr -d com.apple.quarantine /usr/local/bin/uplet 2>/dev/null || true
chmod +x uplet
sudo mv uplet /usr/local/bin/
uplet --version
```

## From source

```sh
git clone https://github.com/ernilambar/uplet
cd uplet
go build -o build/uplet ./cmd/uplet   # -> build/uplet
go install ./cmd/uplet
```
