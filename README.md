# umpv-go
A Go language version of umpv (https://github.com/mpv-player/mpv/blob/master/TOOLS/umpv)

Tested only on Windows, `rsrc.syso` is the icon resource for the Windows program.

## Usage
`umpv video.mkv`

## Options
### --ipc-server
Specify the IPC server socket path. If not provided, a default path will be used based on the operating system.

### --loadfile-flag
Specify the loadfile flag. Possible values are:
- `replace`: Replace the current playlist.
- `append`: Append to the current playlist.
- `append-play`: (default) Append the file, and if nothing is currently playing, start playback.
- `insert-next`: Insert the file into the playlist, directly after the current entry.
- `insert-next-play`: Insert the file next, and if nothing is currently playing, start playback. 
Read [mpv manual](https://mpv.io/manual/master/#command-interface-[%3Coptions%3E]]]) for more details.

### Example
```sh
umpv --ipc-server=\\.\\pipe\\mpvpipe --loadfile-flag=replace video.mkv
```
