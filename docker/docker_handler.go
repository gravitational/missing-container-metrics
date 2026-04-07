package docker

import (
	"context"
	"strings"

	mobyctr "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

func HandleDocker(ctx context.Context, slogger *zap.SugaredLogger) error {
	dc, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return errors.Wrap(err, "while creating docker client")
	}

	eventsResult := dc.Events(ctx, client.EventsListOptions{})
	evts, errs := eventsResult.Messages, eventsResult.Err

	containers, err := dc.ContainerList(ctx, client.ContainerListOptions{
		All: true,
	})

	if err != nil {
		return errors.Wrap(err, "while listing containers")
	}

	h := newEventHandler(func(containerID string) (pod string, namespace string) {
		res, err := dc.ContainerInspect(context.Background(), containerID, client.ContainerInspectOptions{})
		if err != nil {
			return "", ""
		}

		if res.Container.Config == nil {
			return "", ""
		}

		pod = res.Container.Config.Labels["io.kubernetes.pod.name"]
		namespace = res.Container.Config.Labels["io.kubernetes.pod.namespace"]
		return pod, namespace
	})
	for _, c := range containers.Items {
		ci, err := dc.ContainerInspect(ctx, c.ID, client.ContainerInspectOptions{})
		if err != nil {
			slogger.With("container_id", c.ID, "error", err).Warn("while getting container info")
			continue
		}
		cnt := h.addContainer(c.ID, strings.TrimPrefix(c.Names[0], "/"), c.Image)

		if ci.Container.State != nil && ci.Container.State.Status == mobyctr.StateExited {
			cnt.die(ci.Container.State.ExitCode)
		}
	}

	for {
		select {
		case e := <-evts:
			err := h.handle(e)
			if err != nil {
				return errors.Wrapf(err, "while handling event %#v", e)
			}
		case err := <-errs:
			return errors.Wrap(err, "while reading events")
		}
	}

}
