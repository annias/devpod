package docker

import (
	"context"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/loft-sh/devpod/pkg/compose"
	config2 "github.com/loft-sh/devpod/pkg/config"
	"github.com/loft-sh/devpod/pkg/devcontainer/config"
	"github.com/loft-sh/devpod/pkg/docker"
	"github.com/loft-sh/devpod/pkg/driver"
	"github.com/loft-sh/devpod/pkg/ide/jetbrains"
	"github.com/loft-sh/devpod/pkg/log"
	provider2 "github.com/loft-sh/devpod/pkg/provider"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

func NewDockerDriver(workspaceInfo *provider2.AgentWorkspaceInfo, log log.Logger) driver.Driver {
	dockerCommand := "docker"
	if workspaceInfo.Agent.Docker.Path != "" {
		dockerCommand = workspaceInfo.Agent.Docker.Path
	}

	log.Debugf("Using docker command '%s'", dockerCommand)
	dockerHelper := &docker.DockerHelper{DockerCommand: dockerCommand}
	return &dockerDriver{
		Docker: dockerHelper,
		Log:    log,
	}
}

type dockerDriver struct {
	Docker  *docker.DockerHelper
	Compose *compose.ComposeHelper

	Log log.Logger
}

func (d *dockerDriver) CommandDevContainer(ctx context.Context, id, user, command string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	args := []string{"exec"}
	if stdin != nil {
		args = append(args, "-i")
	}
	args = append(args, "-u", user, id, "sh", "-c", command)
	return d.Docker.Run(ctx, args, stdin, stdout, stderr)
}

func (d *dockerDriver) PushDevContainer(ctx context.Context, image string) error {
	// push image
	writer := d.Log.Writer(logrus.InfoLevel, false)
	defer writer.Close()

	// build args
	args := []string{
		"push",
		image,
	}

	// run command
	d.Log.Debugf("Running docker command: docker %s", strings.Join(args, " "))
	err := d.Docker.Run(ctx, args, nil, writer, writer)
	if err != nil {
		return errors.Wrap(err, "push image")
	}

	return nil
}

func (d *dockerDriver) StartDevContainer(ctx context.Context, id string, labels []string) error {
	return d.Docker.StartContainer(ctx, id, labels)
}

func (d *dockerDriver) DeleteDevContainer(ctx context.Context, id string, deleteVolumes bool) error {
	// TODO: implement deleteVolumes

	return d.Docker.Remove(ctx, id)
}

func (d *dockerDriver) StopDevContainer(ctx context.Context, id string) error {
	return d.Docker.Stop(ctx, id)
}

func (d *dockerDriver) InspectImage(ctx context.Context, imageName string) (*config.ImageDetails, error) {
	return d.Docker.InspectImage(imageName, true)
}

func (d *dockerDriver) ComposeHelper() (*compose.ComposeHelper, error) {
	if d.Compose != nil {
		return d.Compose, nil
	}

	var err error
	d.Compose, err = compose.NewComposeHelper(compose.DockerComposeCommand, d.Docker)
	return d.Compose, err
}

func (d *dockerDriver) FindDevContainer(ctx context.Context, labels []string) (*config.ContainerDetails, error) {
	return d.Docker.FindDevContainer(labels)
}

func (d *dockerDriver) RunDevContainer(
	ctx context.Context,
	parsedConfig *config.DevContainerConfig,
	mergedConfig *config.MergedDevContainerConfig,
	imageName,
	workspaceMount string,
	labels []string,
	ide string,
	ideOptions map[string]config2.OptionValue,
	imageDetails *config.ImageDetails,
) error {
	args := []string{
		"run",
		"--sig-proxy=false",
	}

	// add ports
	for _, appPort := range parsedConfig.AppPort {
		intPort, err := strconv.Atoi(appPort)
		if err != nil {
			args = append(args, "-p", appPort)
		} else {
			args = append(args, "-p", fmt.Sprintf("127.0.0.1:%d:%d", intPort, intPort))
		}
	}

	// workspace mount
	if workspaceMount != "" {
		args = append(args, "--mount", workspaceMount)
	}

	// override container user
	if mergedConfig.ContainerUser != "" {
		args = append(args, "-u", mergedConfig.ContainerUser)
	}

	// container env
	for k, v := range mergedConfig.ContainerEnv {
		args = append(args, "-e", k+"="+v)
	}

	// security options
	if mergedConfig.Init != nil && *mergedConfig.Init {
		args = append(args, "--init")
	}
	if mergedConfig.Privileged != nil && *mergedConfig.Privileged {
		args = append(args, "--privileged")
	}
	for _, capAdd := range mergedConfig.CapAdd {
		args = append(args, "--cap-add", capAdd)
	}
	for _, securityOpt := range mergedConfig.SecurityOpt {
		args = append(args, "--security-opt", securityOpt)
	}

	// mounts
	for _, mount := range mergedConfig.Mounts {
		args = append(args, "--mount", mount.String())
	}

	// add ide mounts
	switch ide {
	case string(config2.IDEGoland):
		args = append(args, "--mount", jetbrains.NewGolandServer("", ideOptions, d.Log).GetVolume())
	case string(config2.IDEPyCharm):
		args = append(args, "--mount", jetbrains.NewPyCharmServer("", ideOptions, d.Log).GetVolume())
	case string(config2.IDEPhpStorm):
		args = append(args, "--mount", jetbrains.NewPhpStorm("", ideOptions, d.Log).GetVolume())
	case string(config2.IDEIntellij):
		args = append(args, "--mount", jetbrains.NewIntellij("", ideOptions, d.Log).GetVolume())
	case string(config2.IDECLion):
		args = append(args, "--mount", jetbrains.NewCLionServer("", ideOptions, d.Log).GetVolume())
	case string(config2.IDERider):
		args = append(args, "--mount", jetbrains.NewRiderServer("", ideOptions, d.Log).GetVolume())
	case string(config2.IDERubyMine):
		args = append(args, "--mount", jetbrains.NewRubyMineServer("", ideOptions, d.Log).GetVolume())
	case string(config2.IDEWebStorm):
		args = append(args, "--mount", jetbrains.NewWebStormServer("", ideOptions, d.Log).GetVolume())
	}

	// labels
	for _, label := range labels {
		args = append(args, "-l", label)
	}

	// check GPU
	if parsedConfig.HostRequirements != nil && parsedConfig.HostRequirements.GPU {
		enabled, _ := d.Docker.GPUSupportEnabled()
		if enabled {
			args = append(args, "--gpus", "all")
		}
	}

	// run detached
	args = append(args, "-d")

	// add entrypoint
	entrypoint, cmd := GetContainerEntrypointAndArgs(mergedConfig, imageDetails)
	args = append(args, "--entrypoint", entrypoint)

	// image name
	args = append(args, imageName)

	// entrypoint
	args = append(args, cmd...)

	// run the command
	d.Log.Debugf("Running docker command: docker %s", strings.Join(args, " "))
	writer := d.Log.Writer(logrus.InfoLevel, false)
	defer writer.Close()

	err := d.Docker.RunWithDir(ctx, path.Dir(filepath.ToSlash(parsedConfig.Origin)), args, nil, writer, writer)
	if err != nil {
		return err
	}

	return nil
}

func GetContainerEntrypointAndArgs(mergedConfig *config.MergedDevContainerConfig, imageDetails *config.ImageDetails) (string, []string) {
	customEntrypoints := mergedConfig.Entrypoints
	cmd := []string{"-c", `echo Container started
trap "exit 0" 15
` + strings.Join(customEntrypoints, "\n") + `
exec "$@"
while sleep 1 & wait $!; do :; done`, "-"} // `wait $!` allows for the `trap` to run (synchronous `sleep` would not).
	if mergedConfig.OverrideCommand != nil && !*mergedConfig.OverrideCommand {
		cmd = append(cmd, imageDetails.Config.Entrypoint...)
		cmd = append(cmd, imageDetails.Config.Cmd...)
	}
	return "/bin/sh", cmd
}
