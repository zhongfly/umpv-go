# umpv-go
A Go language version of umpv (https://github.com/mpv-player/mpv/blob/master/TOOLS/umpv)

Only work on Windows, `rsrc.syso` is the icon resource for the Windows program.

## Usage
Place `umpv.exe` in the same directory as `mpv.exe`, or define the path to mpv.exe in the environment variable `MPV`

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

### --config
Specify the config file path. If not provided, the default path `umpv.conf` in the executable directory will be used.

### Example
```sh
umpv --ipc-server=\\.\\pipe\\mpvpipe --loadfile-flag=replace video.mkv
```

## Config
The default configuration file `umpv.conf` in the executable directory will be used, or you can specify the configuration file path using the `--config` command line option.
Refer to the Options section for available settings. Note: Command line arguments will override the corresponding settings in the configuration file.

### Example
```ini
ipc-server=\\.\\pipe\\mpvpipe
loadfile-flag=replace
```
