package main

import (
	"os/exec"
	"runtime"
)

// openInOS asks the OS to open path with its default handler.
// macOS: open, Linux: xdg-open, Windows: rundll32 url.dll.
func openInOS(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", path).Start()
	case "linux":
		return exec.Command("xdg-open", path).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", path).Start()
	default:
		return exec.Command("xdg-open", path).Start()
	}
}

// copyToClipboard writes text to the system clipboard using whatever the host
// provides. macOS: pbcopy, Linux: xclip (Wayland users override via env if
// needed), Windows: clip.exe.
func copyToClipboard(text string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "linux":
		if _, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.Command("xclip", "-selection", "clipboard")
		} else if _, err := exec.LookPath("wl-copy"); err == nil {
			cmd = exec.Command("wl-copy")
		} else {
			return exec.ErrNotFound
		}
	case "windows":
		cmd = exec.Command("clip")
	default:
		return exec.ErrNotFound
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	if _, err := in.Write([]byte(text)); err != nil {
		_ = in.Close()
		return err
	}
	_ = in.Close()
	return cmd.Wait()
}
