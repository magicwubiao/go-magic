package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// containerWorkDir is where the sandbox workDir is bind-mounted inside the
// container. Every path handed to a container command MUST be translated into
// this prefix first: the container has no idea about host paths, so the old
// `cat /host/abs/path` always failed (the file only exists at /workspace/...).
const containerWorkDir = "/workspace"

type DockerSandbox struct {
	image   string
	timeout time.Duration
	workDir string
}

func NewDockerSandbox(image string) *DockerSandbox {
	if image == "" {
		image = "python:3.11-slim"
	}
	return &DockerSandbox{
		image:   image,
		timeout: 30 * time.Second,
		// NOTE: the workDir becomes the container's /workspace via a bind
		// mount, so the default (the shared OS temp dir) effectively exposes
		// the host's /tmp to the container. Callers handling untrusted input
		// should point SetWorkDir at a dedicated, empty directory.
		workDir: getTempDir(),
	}
}

// containerPath maps a sandbox path to its location inside the container.
// The input is always interpreted relative to the sandbox root (matching the
// historical filepath.Join(workDir, p) behaviour, which also treated a leading
// "/" as relative), and anything that would climb out of the mount is rejected.
func (d *DockerSandbox) containerPath(p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", fmt.Errorf("sandbox: empty path")
	}

	slash := filepath.ToSlash(p)
	// Reject traversal explicitly instead of letting path.Clean silently clamp
	// it: "../outside.txt" would otherwise be rewritten to
	// "/workspace/outside.txt" and quietly touch a different file than the
	// caller asked for.
	for _, seg := range strings.Split(slash, "/") {
		if seg == ".." {
			return "", fmt.Errorf("sandbox: path %q escapes the sandbox root", p)
		}
	}

	clean := path.Clean("/" + slash)
	if clean == "/" {
		return "", fmt.Errorf("sandbox: empty path")
	}
	return containerWorkDir + clean, nil
}

func (d *DockerSandbox) Execute(ctx context.Context, cmd string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()

	// Mount workDir to container
	args = append([]string{
		"run",
		"--rm",
		"-v", d.workDir + ":" + containerWorkDir,
		"-w", containerWorkDir,
		d.image,
		cmd,
	}, args...)

	output, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	return output, err
}

func (d *DockerSandbox) ReadFile(p string) ([]byte, error) {
	cpath, err := d.containerPath(p)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), d.timeout)
	defer cancel()

	args := []string{
		"run",
		"--rm",
		"-v", d.workDir + ":" + containerWorkDir,
		d.image,
		"cat", cpath,
	}

	output, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker read file: %w", err)
	}
	return output, nil
}

func (d *DockerSandbox) WriteFile(p string, data []byte) error {
	cpath, err := d.containerPath(p)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), d.timeout)
	defer cancel()

	// Pipe the payload over stdin ("-i") rather than interpolating it into a
	// shell heredoc. The previous `cat > file << 'EOF'` broke whenever the data
	// contained a line equal to the delimiter, and interpolating arbitrary
	// bytes into a shell script is an injection risk. The destination is
	// passed as a positional argument and referenced as "$1", so it is never
	// re-parsed by the shell either.
	args := []string{
		"run",
		"--rm",
		"-i",
		"-v", d.workDir + ":" + containerWorkDir,
		d.image,
		"sh", "-c", `cat > "$1"`, "sh", cpath,
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdin = bytes.NewReader(data)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker write file: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (d *DockerSandbox) Remove(p string) error {
	cpath, err := d.containerPath(p)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), d.timeout)
	defer cancel()

	args := []string{
		"run",
		"--rm",
		"-v", d.workDir + ":" + containerWorkDir,
		d.image,
		"rm", "-f", cpath,
	}

	if out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("docker remove file: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (d *DockerSandbox) SetWorkDir(dir string) {
	d.workDir = dir
}

func (d *DockerSandbox) SetTimeout(timeout time.Duration) {
	d.timeout = timeout
}
