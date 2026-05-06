package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

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
		workDir: getTempDir(),
	}
}

func (d *DockerSandbox) Execute(ctx context.Context, cmd string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()

	// Mount workDir to container
	args = append([]string{
		"run",
		"--rm",
		"-v", d.workDir + ":/workspace",
		"-w", "/workspace",
		d.image,
		cmd,
	}, args...)

	output, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	return output, err
}

func (d *DockerSandbox) ReadFile(path string) ([]byte, error) {
	absPath := filepath.Join(d.workDir, path)

	// Use docker to read file inside container
	args := []string{
		"run",
		"--rm",
		"-v", d.workDir + ":/workspace",
		d.image,
		"cat", filepath.ToSlash(absPath),
	}

	output, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker read file: %w", err)
	}
	return output, nil
}

func (d *DockerSandbox) WriteFile(path string, data []byte) error {
	absPath := filepath.Join(d.workDir, path)

	// Create a temp file locally, then copy to container
	tmpFile := filepath.Join(os.TempDir(), "magic_docker_"+time.Now().Format("20060102_150405")+".tmp")
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return err
	}
	defer os.Remove(tmpFile)

	// Copy to container (simplified - in production use docker cp)
	args := []string{
		"run",
		"--rm",
		"-v", d.workDir + ":/workspace",
		d.image,
		"sh", "-c", "cat > " + filepath.ToSlash(absPath) + " << 'EOF'\n" + string(data) + "\nEOF",
	}

	_, err := exec.Command("docker", args...).CombinedOutput()
	return err
}

func (d *DockerSandbox) Remove(path string) error {
	absPath := filepath.Join(d.workDir, path)

	args := []string{
		"run",
		"--rm",
		"-v", d.workDir + ":/workspace",
		d.image,
		"rm", "-f", filepath.ToSlash(absPath),
	}

	_, err := exec.Command("docker", args...).CombinedOutput()
	return err
}

func (d *DockerSandbox) SetWorkDir(dir string) {
	d.workDir = dir
}

func (d *DockerSandbox) SetTimeout(timeout time.Duration) {
	d.timeout = timeout
}
