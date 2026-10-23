# EleBBS Message Editor

EleEdit is a full-screen ANSI message editor for EleBBS door sessions. Its supported runtime is 32-bit Windows, where it reads and writes through the Winsock handle supplied by EleBBS rather than console standard input or output.

## Downloads

Each release archive includes the executable, this README, `file_id.diz`, and the Q Public License 1.0:

- `eleedit-win32.exe.zip`: Windows x86 (32-bit), supported EleBBS socket transport
- `eleedit-linux-386.zip`: Linux x86 (32-bit)
- `eleedit-linux-amd64.zip`: Linux x86-64
- `eleedit-linux-arm64.zip`: Linux ARM64

The Linux builds are provided for compilation and evaluation. EleBBS socket transport is currently implemented only on Windows, so Linux builds cannot connect to an EleBBS door session.

## Build

Install Go 1.21 or later. For the supported Windows build, run in PowerShell:

```powershell
$env:GOOS = "windows"
$env:GOARCH = "386"
go build -o eleedit-win32.exe .
```

The other release targets use `GOOS=linux` with `GOARCH=386`, `amd64`, or `arm64`.

## Usage

Launch from the BBS node's working directory without a socket option. The editor reads the comm handle from line 2 of `door32.sys`; EleBBS's appended modem/time arguments are ignored. It displays the six positional `msginf` fields (From, To, Subject, message number, area, private flag) and reads or writes only the message body in `msgtmp`; EleBBS owns the header file. Optional settings are read from `eleedit.ini`; quote text is read from `msgquote` when F3 is used. Startup errors are written to `eleedit.log`; errors after socket initialization are also shown to the caller.

CSI and SS3 arrow sequences move the cursor, Home/End jump within a line, and Page Up/Down scroll. Delete removes the character to the right; Backspace removes the character to the left; Ctrl-Y removes the current line. Enter splits a line unless the whole line is `/s`, `/q`, or `/a`: `/s` saves and exits, `/q` inserts the previous message from `msgquote`, and `/a` exits without saving. F2 or Ctrl-S saves without exiting, F3 inserts quote text, and Ctrl-Q or Escape asks whether to save.

Configuration keys are `quote_prefix`, `quote_strip`, `quote_all`, `sig_text`, and `sig_append`. Boolean values accept `yes`, `true`, or `1`.

## License

EleEdit is distributed under the Q Public License version 1.0. See [LICENSE](LICENSE). Complete source is available in this repository.