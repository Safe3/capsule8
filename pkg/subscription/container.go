package subscription

import (
	"strings"
	"sync"

	api "github.com/capsule8/api/v0"
	"github.com/capsule8/reactive8/pkg/container"
)

func newContainerCreated(cID string) *api.ContainerEvent {
	ev := &api.ContainerEvent{
		Type: api.ContainerEventType_CONTAINER_EVENT_TYPE_CREATED,
	}

	return ev
}

func newContainerRunning(cID string) *api.ContainerEvent {
	ev := &api.ContainerEvent{
		Type: api.ContainerEventType_CONTAINER_EVENT_TYPE_RUNNING,
	}

	return ev
}

func newContainerExited(cID string) *api.ContainerEvent {
	ev := &api.ContainerEvent{
		Type: api.ContainerEventType_CONTAINER_EVENT_TYPE_EXITED,
	}

	return ev
}

func newContainerDestroyed(cID string) *api.ContainerEvent {
	ev := &api.ContainerEvent{
		Type: api.ContainerEventType_CONTAINER_EVENT_TYPE_DESTROYED,
	}

	return ev
}

//
// We get two ContainerCreated events, use this to uniq them
//
var containerCreated map[string]*api.ContainerEvent = make(map[string]*api.ContainerEvent)

//
// We get two ContainerStarted events from the container EventStream:
// one from Docker and one from OCI. We use this map to merge them.
//
var containerStarted map[string]*api.ContainerEvent = make(map[string]*api.ContainerEvent)

//
// Maps IDs of all extant containers
//
var containerCache map[string]*api.ContainerEvent

//
// Lock to protect the started container cache
//
var containerCacheLock sync.Mutex

//
// "Once" control for initializing the container cache
//
var containerCacheOnce sync.Once

func newContainerCache() (map[string]*api.ContainerEvent, error) {
	dockerConfigList, err := container.GetDockerConfigList()
	if err != nil {
		return nil, err
	}

	contCache := make(map[string]*api.ContainerEvent)
	for _, dockerConfig := range dockerConfigList {
		ce := &api.ContainerEvent{
			Type:      api.ContainerEventType_CONTAINER_EVENT_TYPE_UNKNOWN,
			Name:      dockerConfig.Name,
			ImageId:   dockerConfig.Image,
			ImageName: dockerConfig.Config.Image,
		}
		contCache[dockerConfig.ID] = ce
	}

	return contCache, nil
}

func translateContainerEvents(e interface{}) interface{} {
	ce := e.(*container.Event)
	var ece *api.ContainerEvent

	switch ce.State {
	case container.ContainerCreated:
		if containerCreated == nil {
			containerCreated = make(map[string]*api.ContainerEvent)
		}

		if containerCreated[ce.ID] != nil {
			ece = containerCreated[ce.ID]
		} else {
			ece = newContainerCreated(ce.ID)
			ece.Name = ce.Name
			ece.ImageId = ce.ImageID
			ece.ImageName = ce.Image
		}

		if len(ce.DockerConfig) > len(ece.DockerConfigJson) {
			ece.DockerConfigJson = ce.DockerConfig
		}

		if containerCreated[ce.ID] == nil {
			containerCreated[ce.ID] = ece
			saveContainerEvent(ce.ID, ece)
			ece = nil
		} else {
			delete(containerCreated, ce.ID)
		}

	case container.ContainerStarted:
		if containerStarted == nil {
			containerStarted = make(map[string]*api.ContainerEvent)
		}

		if containerStarted[ce.ID] != nil {
			//
			// If we have already received one container
			// started event, merge the 2nd one into it
			//
			ece = containerStarted[ce.ID]
		} else {
			ece = newContainerRunning(ce.ID)
		}

		if ce.Pid != 0 {
			ece.HostPid = int32(ce.Pid)
		}

		if len(ce.DockerConfig) > 0 {
			ece.DockerConfigJson = ce.DockerConfig
		}

		if len(ce.OciConfig) > 0 {
			ece.OciConfigJson = ce.OciConfig
		}

		if containerStarted[ce.ID] == nil {
			containerStarted[ce.ID] = ece
			saveContainerEvent(ce.ID, ece)
			ece = nil
		} else {
			delete(containerStarted, ce.ID)
		}

	case container.ContainerStopped:
		ece = newContainerExited(ce.ID)

		if ce.Pid != 0 {
			ece.HostPid = int32(ce.Pid)
		}

		if len(ce.DockerConfig) > 0 {
			ece.DockerConfigJson = ce.DockerConfig
		}

		ece.Name = ce.Name
		ece.ImageId = ce.ImageID
		ece.ImageName = ce.Image
		ece.ExitCode = ce.ExitCode

	case container.ContainerRemoved:
		ece = newContainerDestroyed(ce.ID)

	default:
		panic("Invalid value for ContainerState")
	}

	if ece != nil {
		if len(ce.DockerConfig) > 0 {
			ece.DockerConfigJson = ce.DockerConfig
		}

		if len(ce.OciConfig) > 0 {
			ece.OciConfigJson = ce.OciConfig
		}

		saveContainerEvent(ce.ID, ece)

		ev := newEventFromContainer(ce.ID)
		ev.ContainerName = ece.Name
		ev.ImageId = ece.ImageId
		ev.ImageName = ece.ImageName
		ev.Event = &api.Event_Container{
			Container: ece,
		}

		return ev
	}

	return nil
}

// Saves a container event in the cache
func saveContainerEvent(contId string, ce *api.ContainerEvent) {
	containerCacheOnce.Do(func() {
		containerCache, _ = newContainerCache()
	})

	containerCacheLock.Lock()
	containerCache[contId] = ce
	containerCacheLock.Unlock()
}

func trimImageIdPrefix(ImageId string) string {
	if strings.HasPrefix(ImageId, "sha256:") {
		ImageId = ImageId[7:]
	}
	return ImageId
}

// Gets the container event for a given ID, looking in the cache first.
func getContainerEvent(contId string) (*api.ContainerEvent, error) {
	containerCacheOnce.Do(func() {
		containerCache, _ = newContainerCache()
	})

	containerCacheLock.Lock()
	ce, ok := containerCache[contId]
	containerCacheLock.Unlock()

	if !ok {
		dc, err := container.GetDockerConfig(contId)
		if err != nil {
			return nil, err
		}

		ce = &api.ContainerEvent{
			Name:      dc.Name,
			ImageId:   trimImageIdPrefix(dc.Image),
			ImageName: dc.Config.Image,
		}
		saveContainerEvent(contId, ce)
	}
	return nil, nil
}