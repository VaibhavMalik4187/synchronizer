package incluster

import (
	"context"
	"fmt"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/kubescape/go-logger"
	"github.com/kubescape/go-logger/helpers"
	"github.com/kubescape/synchronizer/adapters"
	"github.com/kubescape/synchronizer/config"
	"github.com/kubescape/synchronizer/domain"
	"github.com/kubescape/synchronizer/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
)

type Client struct {
	client        dynamic.Interface
	account       string
	cluster       string
	kind          *domain.Kind
	callbacks     domain.Callbacks
	res           schema.GroupVersionResource
	shadowObjects map[string][]byte
	Strategy      domain.Strategy
}

func NewClient(client dynamic.Interface, account, cluster string, r config.Resource) *Client {
	res := schema.GroupVersionResource{Group: r.Group, Version: r.Version, Resource: r.Resource}
	return &Client{
		account: account,
		client:  client,
		cluster: cluster,
		kind: &domain.Kind{
			Group:    res.Group,
			Version:  res.Version,
			Resource: res.Resource,
		},
		res:           res,
		shadowObjects: map[string][]byte{},
		Strategy:      r.Strategy,
	}
}

var _ adapters.Client = (*Client)(nil)

func (c *Client) Start(ctx context.Context) error {
	watchOpts := metav1.ListOptions{}
	// for our storage, we need to list all resources and get them one by one
	// as list returns objects with empty spec
	// and watch does not return existing objects
	if c.res.Group == "spdx.softwarecomposition.kubescape.io" {
		list, err := c.client.Resource(c.res).Namespace("").List(context.Background(), metav1.ListOptions{})
		if err != nil {
			return fmt.Errorf("list resources: %w", err)
		}
		for _, d := range list.Items {
			ctx := utils.ContextFromGeneric(ctx, domain.Generic{})
			id := domain.KindName{
				Kind:      c.kind,
				Name:      d.GetName(),
				Namespace: d.GetNamespace(),
			}
			obj, err := c.client.Resource(c.res).Namespace(d.GetNamespace()).Get(context.Background(), d.GetName(), metav1.GetOptions{})
			if err != nil {
				logger.L().Ctx(ctx).Error("cannot get object", helpers.Error(err), helpers.String("id", id.String()))
				continue
			}
			newObject, err := obj.MarshalJSON()
			if err != nil {
				logger.L().Ctx(ctx).Error("cannot marshal object", helpers.Error(err), helpers.String("id", id.String()))
				continue
			}
			err = c.callVerifyObject(ctx, id, newObject)
			if err != nil {
				logger.L().Ctx(ctx).Error("cannot handle added resource", helpers.Error(err), helpers.String("id", id.String()))
				continue
			}
		}
		// set resource version to watch from
		watchOpts.ResourceVersion = list.GetResourceVersion()
	}
	// begin watch
	watcher, err := c.client.Resource(c.res).Namespace("").Watch(context.Background(), watchOpts)
	if err != nil {
		logger.L().Ctx(ctx).Fatal("unable to watch for resources", helpers.String("resource", c.res.Resource), helpers.Error(err))
	}
	eventQueue := utils.NewCooldownQueue(utils.DefaultQueueSize, utils.DefaultTTL)
	go func() {
		for {
			event, chanActive := <-watcher.ResultChan()
			if !chanActive {
				watcher.Stop()
				eventQueue.Stop()
				break
			}
			eventQueue.Enqueue(event)
		}
	}()
	for event := range eventQueue.ResultChan {
		ctx := utils.ContextFromGeneric(ctx, domain.Generic{})
		if event.Type == watch.Error {
			logger.L().Ctx(ctx).Error("watch event failed", helpers.String("resource", c.res.Resource), helpers.Interface("event", event))
			watcher.Stop()
			break
		}
		d, ok := event.Object.(*unstructured.Unstructured)
		if !ok {
			continue
		}
		id := domain.KindName{
			Kind:      c.kind,
			Name:      d.GetName(),
			Namespace: d.GetNamespace(),
		}
		newObject, err := d.MarshalJSON()
		if err != nil {
			logger.L().Ctx(ctx).Error("cannot marshal object", helpers.Error(err), helpers.String("resource", c.res.Resource), helpers.String("id", id.String()))
			continue
		}
		switch {
		case event.Type == watch.Added:
			logger.L().Debug("added resource", helpers.String("id", id.String()))
			err := c.callVerifyObject(ctx, id, newObject)
			if err != nil {
				logger.L().Ctx(ctx).Error("cannot handle added resource", helpers.Error(err), helpers.String("id", id.String()))
			}
		case event.Type == watch.Deleted:
			logger.L().Debug("deleted resource", helpers.String("id", id.String()))
			err := c.callbacks.DeleteObject(ctx, id)
			if err != nil {
				logger.L().Ctx(ctx).Error("cannot handle deleted resource", helpers.Error(err), helpers.String("id", id.String()))
			}
			if c.Strategy == domain.PatchStrategy {
				// remove from known resources
				delete(c.shadowObjects, id.Name)
			}
		case event.Type == watch.Modified:
			logger.L().Debug("modified resource", helpers.String("id", id.String()))
			err := c.callPutOrPatch(ctx, id, nil, newObject)
			if err != nil {
				logger.L().Ctx(ctx).Error("cannot handle modified resource", helpers.Error(err), helpers.String("id", id.String()))
			}
		}
	}
	return nil
}

func (c *Client) callPutOrPatch(ctx context.Context, id domain.KindName, baseObject []byte, newObject []byte) error {
	if c.Strategy == domain.PatchStrategy {
		if len(baseObject) > 0 {
			// update reference object
			c.shadowObjects[id.Name] = baseObject
		}
		if oldObject, ok := c.shadowObjects[id.Name]; ok {
			// calculate checksum
			checksum, err := utils.CanonicalHash(newObject)
			if err != nil {
				return fmt.Errorf("calculate checksum: %w", err)
			}
			// calculate patch
			patch, err := jsonpatch.CreateMergePatch(oldObject, newObject)
			if err != nil {
				return fmt.Errorf("create merge patch: %w", err)
			}
			err = c.callbacks.PatchObject(ctx, id, checksum, patch)
			if err != nil {
				return fmt.Errorf("send patch object: %w", err)
			}
		} else {
			err := c.callbacks.PutObject(ctx, id, newObject)
			if err != nil {
				return fmt.Errorf("send put object: %w", err)
			}
		}
		// add/update known resources
		c.shadowObjects[id.Name] = newObject
	} else {
		err := c.callbacks.PutObject(ctx, id, newObject)
		if err != nil {
			return fmt.Errorf("send put object: %w", err)
		}
	}
	return nil
}

func (c *Client) callVerifyObject(ctx context.Context, id domain.KindName, object []byte) error {
	// calculate checksum
	checksum, err := utils.CanonicalHash(object)
	if err != nil {
		return fmt.Errorf("calculate checksum: %w", err)
	}
	err = c.callbacks.VerifyObject(ctx, id, checksum)
	if err != nil {
		return fmt.Errorf("send checksum: %w", err)
	}
	return nil
}

func (c *Client) DeleteObject(_ context.Context, id domain.KindName) error {
	if c.Strategy == domain.PatchStrategy {
		// remove from known resources
		delete(c.shadowObjects, id.String())
	}
	return c.client.Resource(c.res).Namespace(id.Namespace).Delete(context.Background(), id.Name, metav1.DeleteOptions{})
}

func (c *Client) GetObject(ctx context.Context, id domain.KindName, baseObject []byte) error {
	obj, err := c.client.Resource(c.res).Namespace(id.Namespace).Get(context.Background(), id.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get resource: %w", err)
	}
	newObject, err := obj.MarshalJSON()
	if err != nil {
		return fmt.Errorf("marshal resource: %w", err)
	}
	return c.callPutOrPatch(ctx, id, baseObject, newObject)
}

func (c *Client) PatchObject(ctx context.Context, id domain.KindName, checksum string, patch []byte) error {
	baseObject, err := c.patchObject(ctx, id, checksum, patch)
	if err != nil {
		logger.L().Warning("patch object, sending get object", helpers.Error(err), helpers.String("id", id.String()))
		return c.callbacks.GetObject(ctx, id, baseObject)
	}
	return nil
}

func (c *Client) patchObject(ctx context.Context, id domain.KindName, checksum string, patch []byte) ([]byte, error) {
	if c.Strategy != domain.PatchStrategy {
		return nil, fmt.Errorf("patch strategy not enabled for resource %s", id.Kind.String())
	}
	obj, err := c.client.Resource(c.res).Namespace(id.Namespace).Get(context.Background(), id.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get resource: %w", err)
	}
	object, err := obj.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("marshal resource: %w", err)
	}
	// apply patch
	modified, err := jsonpatch.MergePatch(object, patch)
	if err != nil {
		return object, fmt.Errorf("apply patch: %w", err)
	}
	// verify checksum
	newChecksum, err := utils.CanonicalHash(modified)
	if err != nil {
		return object, fmt.Errorf("calculate checksum: %w", err)
	}
	if newChecksum != checksum {
		return object, fmt.Errorf("checksum mismatch: %s != %s", newChecksum, checksum)
	}
	// update known resources
	c.shadowObjects[id.Name] = modified
	// save object
	return object, c.PutObject(ctx, id, modified)
}

func (c *Client) PutObject(_ context.Context, _ domain.KindName, object []byte) error {
	var obj unstructured.Unstructured
	err := obj.UnmarshalJSON(object)
	if err != nil {
		return fmt.Errorf("unmarshal object: %w", err)
	}
	_, err = c.client.Resource(c.res).Namespace("").Create(context.Background(), &obj, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("create resource: %w", err)
	}
	return nil
}

func (c *Client) RegisterCallbacks(_ context.Context, callbacks domain.Callbacks) {
	c.callbacks = callbacks
}

func (c *Client) Callbacks(_ context.Context) (domain.Callbacks, error) {
	return c.callbacks, nil
}

func (c *Client) VerifyObject(ctx context.Context, id domain.KindName, newChecksum string) error {
	baseObject, err := c.verifyObject(id, newChecksum)
	if err != nil {
		logger.L().Warning("verify object, sending get object", helpers.Error(err), helpers.String("id", id.String()))
		return c.callbacks.GetObject(ctx, id, baseObject)
	}
	return nil
}

func (c *Client) verifyObject(id domain.KindName, newChecksum string) ([]byte, error) {
	obj, err := c.client.Resource(c.res).Namespace(id.Namespace).Get(context.Background(), id.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get resource: %w", err)
	}
	object, err := obj.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("marshal resource: %w", err)
	}
	checksum, err := utils.CanonicalHash(object)
	if err != nil {
		return object, fmt.Errorf("calculate checksum: %w", err)
	}
	if checksum != newChecksum {
		return object, fmt.Errorf("checksum mismatch: %s != %s", newChecksum, checksum)
	}
	return object, nil
}
