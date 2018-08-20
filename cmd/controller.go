/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cmd

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"os"
	"runtime/debug"

	"github.com/golang/glog"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	extendingressv1alpha1 "k8s.io/extendingress-controller/pkg/apis/extendingresscontroller/v1alpha1"
	clientset "k8s.io/extendingress-controller/pkg/client/clientset/versioned"
	extendingresscheme "k8s.io/extendingress-controller/pkg/client/clientset/versioned/scheme"
	informers "k8s.io/extendingress-controller/pkg/client/informers/externalversions/extendingresscontroller/v1alpha1"
	listers "k8s.io/extendingress-controller/pkg/client/listers/extendingresscontroller/v1alpha1"
)

const controllerAgentName = "extendingress-controller"

const (
	// SuccessSynced is used as part of the Event 'reason' when a Extendingress is synced
	SuccessSynced = "Synced"
	// ErrResourceExists is used as part of the Event 'reason' when a Extendingress fails
	// to sync due to a Deployment of the same name already existing.
	ErrResourceExists = "ErrResourceExists"

	// MessageResourceExists is the message used for Events when a resource
	// fails to sync due to a Deployment already existing
	MessageResourceExists = "Resource %q already exists and is not managed by Extendingress"
	// MessageResourceSynced is the message used for an Event fired when a Extendingress
	// is synced successfully
	MessageResourceSynced = "Extendingress synced successfully"

	ExtendIngressADD    = "ExtendIngressADD"
	ExtendIngressUpdate = "ExtendIngressUpdate"
	ExtendIngressDel    = "ExtendIngressDel"

	EndpointADD    = "EndpointADD"
	EndpointUpdate = "EndpointUpdate"
	EndpointDel    = "EndpointDel"

	ConfigmapADD    = "ConfigmapADD"
	ConfigmapUpdate = "ConfigmapUpdate"
	ConfigmapDel    = "ConfigmapDel"

	MILLION = 1000000
)

// Controller is the controller implementation for Extendingress resources
type Controller struct {
	// kubeclientset is a standard kubernetes clientset
	kubeclientset kubernetes.Interface
	// sampleclientset is a clientset for our own API group
	extendIngresslientset clientset.Interface

	endpointLister      corelisters.EndpointsLister
	endpointSynced      cache.InformerSynced
	configmapLister     corelisters.ConfigMapLister
	configmapSynced     cache.InformerSynced
	extendIngressLister listers.ExtendIngressLister
	extendIngressSynced cache.InformerSynced
	extendIngressStore  cache.Store
	extendIngressCfg    ExtendIngressCfg
	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	workqueue workqueue.RateLimitingInterface
	// recorder is an event recorder for recording Event resources to the
	// Kubernetes API.
	recorder record.EventRecorder
}

func NewController(
	kubeclientset kubernetes.Interface,
	extendIngresslientset clientset.Interface,
	endpointInformer v1.EndpointsInformer,
	configmapInformer v1.ConfigMapInformer,
	extendIngressInformer informers.ExtendIngressInformer,
	config *Configuration) *Controller {

	// Create event broadcaster
	// Add extendingress-controller types to the default Kubernetes Scheme so Events can be
	// logged for extendingress-controller types.
	extendingresscheme.AddToScheme(scheme.Scheme)
	glog.V(4).Info("Creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeclientset.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName})

	controller := &Controller{
		kubeclientset:         kubeclientset,
		extendIngresslientset: extendIngresslientset,
		endpointSynced:        endpointInformer.Informer().HasSynced,
		endpointLister:        endpointInformer.Lister(),
		configmapSynced:       configmapInformer.Informer().HasSynced,
		configmapLister:       configmapInformer.Lister(),
		extendIngressLister:   extendIngressInformer.Lister(),
		extendIngressSynced:   extendIngressInformer.Informer().HasSynced,
		extendIngressStore:    extendIngressInformer.Informer().GetStore(),
		extendIngressCfg: ExtendIngressCfg{
			CommonConf: make(map[string]string),
			HttpConf:   make(map[string]string),
			EventConf:  make(map[string]string),
			Servers:    make(map[string]*Server),
			Upstreams:  make(map[string]*Upstream),
			Start:      true,
			Config:     *config,
		},
		workqueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "ExtendIngresses"),
		recorder:  recorder,
	}

	glog.Info("Setting up event handlers")
	// Set up an event handler for when ExtendIngress resources change
	extendIngressInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			controller.enqueueExtendIngress(obj, ExtendIngressADD+"/")
		},
		UpdateFunc: func(old, new interface{}) {
			oldEx := old.(*extendingressv1alpha1.ExtendIngress)
			newEx := new.(*extendingressv1alpha1.ExtendIngress)
			if oldEx.ResourceVersion == newEx.ResourceVersion {
				return
			}
			if reflect.DeepEqual(oldEx.Spec, newEx.Spec) {
				return
			}
			controller.enqueueExtendIngress(new, ExtendIngressUpdate+"/")
		},
		DeleteFunc: func(obj interface{}) {
			controller.enqueueExtendIngress(obj, ExtendIngressDel+"/")
		},
	})

	// Set up an event handler for when Endpoint resources change
	endpointInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			ep := obj.(*corev1.Endpoints)
			if ep.ObjectMeta.Name == "kubernetes" {
				return
			}
			if ep.Subsets == nil {
				return
			}
			epStrings, err := controller.handleEndpointObj(obj)
			if err != nil {
				glog.Error("error when ep fliter", err)
			}
			if len(epStrings) > 0 {
				controller.enqueueEndpoint(epStrings, EndpointADD+"/")
			}
		},
		UpdateFunc: func(old, new interface{}) {
			newEp := new.(*corev1.Endpoints)
			oldEp := old.(*corev1.Endpoints)
			if newEp.Namespace == "kube-system" || (oldEp.Subsets == nil && newEp.Subsets == nil) {
				return
			}
			if reflect.DeepEqual(oldEp.Subsets, newEp.Subsets) {
				// Periodic resync will send update events for all known Deployments.
				// Two different subsets of the same Endpoint will always have different RVs.
				return
			}

			epStrings, err := controller.handleEndpointObj(new)
			if err != nil {
				glog.Error("error when ep fliter", err)
			}
			if len(epStrings) > 0 {
				controller.enqueueEndpoint(epStrings, EndpointUpdate+"/")
			}
		},
		DeleteFunc: func(obj interface{}) {
			epStrings, err := controller.handleEndpointObj(obj)
			if err != nil {
				glog.Error("error when ep fliter", err)
			}
			if len(epStrings) > 0 {
				controller.enqueueEndpoint(epStrings, EndpointDel+"/")
			}
		},
	})

	// Set up an event handler for when Configmap resources change
	configmapInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			cmStr, err := controller.handleConfigmapObj(obj, *config.CommonConf, *config.EventConf, *config.HttpConf)
			if err != nil {
				glog.Error("error when cm fliter", err)
			}
			if cmStr != "" {
				controller.workqueue.AddRateLimited(ConfigmapADD + cmStr)
			}
		},
		UpdateFunc: func(old, new interface{}) {
			newCm := new.(*corev1.ConfigMap)
			oldCm := old.(*corev1.ConfigMap)

			if reflect.DeepEqual(newCm.Data, oldCm.Data) {
				// Periodic resync will send update events for all known Deployments.
				// Two different subsets of the same Endpoint will always have different RVs.
				return
			}

			cmStr, err := controller.handleConfigmapObj(new, *config.CommonConf, *config.EventConf, *config.HttpConf)
			if err != nil {
				glog.Error("error when cm fliter", err)
			}
			if cmStr != "" {
				controller.workqueue.AddRateLimited(ConfigmapUpdate + cmStr)
			}
		},
		DeleteFunc: func(obj interface{}) {
			cmStr, err := controller.handleConfigmapObj(obj, *config.CommonConf, *config.EventConf, *config.HttpConf)
			if err != nil {
				glog.Error("error when cm fliter", err)
			}
			if cmStr != "" {
				controller.workqueue.AddRateLimited(ConfigmapDel + cmStr)
			}
		},
	})
	return controller
}

// enqueueExtendingress takes a ExtendIngress resource and converts it into a ExtendIngressAdd|Update|Del
// namespace/name string which is then put onto the work queue. This method should *not* be
// passed resources of any type other than ExtendIngress.
func (c *Controller) enqueueExtendIngress(obj interface{}, event string) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		runtime.HandleError(err)
		return
	}
	if strings.Contains(event, "Del") {
		ingress := obj.(*extendingressv1alpha1.ExtendIngress)
		for _, rule := range ingress.Spec.Rules {
			serverNameLoc := "/" + rule.Host
			for _, path := range rule.HTTP.Paths {
				serverNameLoc += "-" + path.Path[1:]
			}
			key += serverNameLoc
		}
	}
	c.workqueue.AddRateLimited(event + key)
}

// enqueueEndpoint takes a endpoint resource and converts it into a EndpointAdd|Update|Del namespace/name
// string which is then put onto the work queue. This method should *not* be
// passed resources of any type other than ExtendIngress.
func (c *Controller) enqueueEndpoint(epStrings []string, event string) {
	for _, epstring := range epStrings {
		c.workqueue.AddRateLimited(event + epstring)
	}
}

// handleEndpointObj will take any resource implementing metav1.Object and attempt
// to find the ExtendIngress resource that 'belong' it. It does this by listing all
// ExtendIngress resource and find the field in Rules to find the map with ExtendIngress
// and Service.It then enqueues that ExtendIngress resource to be processed. If the
// object does not have an appropriate mapping, it will simply be skipped.
func (c *Controller) handleEndpointObj(obj interface{}) ([]string, error) {
	//var object metav1.Object
	var epString []string

	if object, ok := obj.(*corev1.Endpoints); ok {
		namespace := object.GetNamespace()
		name := object.GetName()
		extendIngress := c.extendIngressStore.List()
		for _, ingress := range extendIngress {
			ingress := ingress.(*extendingressv1alpha1.ExtendIngress)
			if ingress.GetNamespace() == namespace {
				if len(ingress.Spec.Rules) > 0 {
					for _, rule := range ingress.Spec.Rules {
						if len(rule.HTTP.Paths) > 0 {
							for _, path := range rule.HTTP.Paths {
								if path.Backend.ServiceName == name {
									epString = append(epString, getProxyPass(namespace, ingress.Name,
										name, path.Backend.ServicePort.String(), path.Path))
								}
							}
						}
					}
				}
			}
		}
		return epString, nil
	}
	return nil, nil
}

// handleConfigmapObj will take any resource implementing metav1.Object and attempt
// to find the configmap resource that 'belong' it. It does this by listing all
// configmap resource and find the field in Rules to find the map with ExtendIngress
// and configmap.It then enqueues that configmap resource to be processed. If the
// object does not have an appropriate mapping, it will simply be skipped.
func (c *Controller) handleConfigmapObj(obj interface{}, commonConf, eventConf, httpConf string) (string, error) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		runtime.HandleError(err)
		return "", err
	}
	// strings.Contains(commonConf, key)
	if commonConf == key {
		return "-common/" + key, nil
	} else if eventConf == key {
		return "-event/" + key, nil
	} else if httpConf == key {
		return "-http/" + key, nil
	}
	return "", nil
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	defer runtime.HandleCrash()
	defer c.workqueue.ShutDown()

	// Start the informer factories to begin populating the informer caches
	glog.Info("Starting Extendingress controller")

	// Wait for the caches to be synced before starting workers
	glog.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.endpointSynced, c.configmapSynced, c.extendIngressSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	glog.Info("Starting workers")
	// Launch two workers to process Extendingress resources
	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}
	<-stopCh
	glog.Info("Shutting down workers")

	return nil
}

// runWorker is a long-running function that will continually call the
// processNextWorkItem function in order to read and process a message on the
// workqueue.
func (c *Controller) runWorker() {
	for c.processNextWorkItem() {
	}
}

// processNextWorkItem will read a single work item off the workqueue and
// attempt to process it, by calling the syncHandler.
func (c *Controller) processNextWorkItem() bool {
	obj, shutdown := c.workqueue.Get()

	if shutdown {
		return false
	}

	// We wrap this block in a func so we can defer c.workqueue.Done.
	err := func(obj interface{}) error {
		// We call Done here so the workqueue knows we have finished
		// processing this item. We also must remember to call Forget if we
		// do not want this work item being re-queued. For example, we do
		// not call Forget if a transient error occurs, instead the item is
		// put back on the workqueue and attempted again after a back-off
		// period.
		defer c.workqueue.Done(obj)
		var key string
		var ok bool
		// We expect strings to come off the workqueue. These are of the
		// form namespace/name. We do this as the delayed nature of the
		// workqueue means the items in the informer cache may actually be
		// more up to date that when the item was initially put onto the
		// workqueue.
		if key, ok = obj.(string); !ok {
			// As the item in the workqueue is actually invalid, we call
			// Forget here else we'd go into a loop of attempting to
			// process a work item that is invalid.
			c.workqueue.Forget(obj)
			runtime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		// Run the syncHandler, passing it the namespace/name string of the
		// Extendingress resource to be synced.
		if err := c.syncHandler(key); err != nil {
			return fmt.Errorf("error syncing '%s': %s", key, err.Error())
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		c.workqueue.Forget(obj)
		glog.Infof("Successfully synced '%s'", key)
		return nil
	}(obj)

	if err != nil {
		runtime.HandleError(err)
		return true
	}

	return true
}

// syncHandler compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Extendingress resource
// with the current status of the resource.
func (c *Controller) updateExtendIngressStatus(ingress *extendingressv1alpha1.ExtendIngress, nginxTestStr string) *extendingressv1alpha1.ExtendIngress {
	// NEVER modify objects from the store. It's a read-only, local cache.
	// You can use DeepCopy() to make a deep copy of original object and modify this copy
	// Or create a copy manually for better performance
	ingressCopy := ingress.DeepCopy()
	ingressCopy.Status.LastPaths = make(map[string][]extendingressv1alpha1.ExtendHTTPIngressPath)
	podNamespace := os.Getenv("POD_NAMESPACE")
	podName := os.Getenv("POD_NAME")
	if len(podName) > 0 && len(podNamespace) > 0 {
		pod, err := c.kubeclientset.CoreV1().Pods(podNamespace).Get(podName, metav1.GetOptions{})
		if err != nil {
			glog.Error(err.Error())
		} else {
			if podRefer, ok := pod.Annotations["kubernetes.io/created-by"]; ok {
				mapStr := make(map[string]interface{})
				json.Unmarshal([]byte(podRefer), &mapStr)
				if referMap, ok := mapStr["reference"]; ok {
					reference := referMap.(map[string]interface{})
					if reference["kind"].(string) != "DaemonSet" {
						glog.Error("the pod deploy type is not by DaemonSet!")
					}
					daemonSetName := reference["name"].(string)
					daemonSetNamespace := reference["namespace"].(string)
					damonSet, err := c.kubeclientset.AppsV1beta2().DaemonSets(daemonSetNamespace).Get(daemonSetName, metav1.GetOptions{})
					if err != nil {
						glog.Error(err.Error())
					}

					pods, err := c.kubeclientset.CoreV1().Pods(podNamespace).List(metav1.ListOptions{})
					var matchingPods []extendingressv1alpha1.LoadBalancerIngress
					for _, pod := range pods.Items {
						if pod.ObjectMeta.Namespace == damonSet.Namespace &&
							c.isLabelSelectorMatching(pod.Labels, damonSet.Spec.Selector) {
							matchingPods = append(matchingPods, extendingressv1alpha1.LoadBalancerIngress{IP: pod.Status.PodIP})
						}
					}
					ingressCopy.Status.LoadBalancer.Ingress = matchingPods
				}
			}
		}
	}
	// If the CustomResourceSubresources feature gate is not enabled,
	// we must use Update instead of UpdateStatus to update the Status block of the Foo resource.
	// UpdateStatus will not allow changes to the Spec of the resource,
	// which is ideal for ensuring nothing other than resource status has been updated.
	for _, rule := range ingress.Spec.Rules {
		for _, ingressPath := range rule.HTTP.Paths {
			ingressCopy.Status.LastPaths[rule.Host] = append(ingressCopy.Status.LastPaths[rule.Host], ingressPath)
		}
	}
	if strings.Contains(nginxTestStr, "test failed") {
		ingressCopy.Status.State = "unReady"
	} else if strings.Contains(nginxTestStr, "successful") {
		ingressCopy.Status.State = "Ready"
	}
	ingressCopy.Status.NginxIngressStatus = nginxTestStr
	return ingressCopy
}

// IsLabelSelectorMatching returns true when a resource with the given selector targets the same
// Resources(or subset) that a tested object selector with the given selector.
func (c *Controller) isLabelSelectorMatching(selector map[string]string, labelSelector *metav1.LabelSelector) bool {
	// If the resource has no selectors, then assume it targets different Pods.
	if len(selector) == 0 {
		return false
	}
	for label, value := range selector {
		if rsValue, ok := labelSelector.MatchLabels[label]; !ok || rsValue != value {
			return false
		}
	}
	return true
}

// syncHandler compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the ExtendIngress resource
// with the current status of the resource.
func (c *Controller) syncHandler(key string) error {
	startTime := time.Now()
	// define defer to catch panic
	defer func() {
		glog.V(2).Infof("Finished syncing event %s. (%v)", key, time.Now().Sub(startTime).Nanoseconds()/MILLION)
		if err := recover(); err != nil {
			debug.PrintStack()
			return
		}
	}()
	var splitStr []string

	if strings.Contains(key, "ExtendIngress") {
		splitStr = strings.Split(key, "/")
		if strings.Contains(key, "Del") {
			c.extendIngressCfg.delServerAndUpstreamFromCfg(splitStr[3:])
		} else {

			ingress, err := c.extendIngressLister.ExtendIngresses(splitStr[1]).Get(splitStr[2])
			if err != nil {
				// The ExtendIngress resource may no longer exist, in which case we stop
				// processing.
				if errors.IsNotFound(err) {
					runtime.HandleError(fmt.Errorf("ExtendIngress '%s' in work queue no longer exists", key))
					return nil
				}
				return err
			}
			servers, upstreams, nginxTestStr, err := c.extendIngressCfg.parseExtendIngressIntoCfg(ingress, c.endpointLister)
			if err != nil {
				nginxTestStr = strings.Replace(err.Error(), "\r\n", "\n", -1)
			}

			updateIngress := c.updateExtendIngressStatus(ingress, nginxTestStr)
			if updateIngress.Status.State == "Ready" {
				glog.V(4).Info(nginxTestStr)
				c.extendIngressCfg.updateServerAndUpstreamToCfg(servers, upstreams, ingress.Status.LastPaths, splitStr[1], splitStr[2])
			} else {
				glog.Error(err.Error())
			}

			_, err = c.extendIngresslientset.ExtendingresscontrollerV1alpha1().ExtendIngresses(splitStr[1]).Update(updateIngress)
			if err != nil {
				// The ExtendIngress resource may no longer exist, in which case we stop
				// processing.
				if errors.IsNotFound(err) {
					runtime.HandleError(fmt.Errorf("ExtendIngress '%s/%s' update error, %s", splitStr[1], splitStr[2], err.Error()))
				}
			}
		}
	} else if strings.Contains(key, "Endpoint") {

		proxyStr := strings.Split(key[strings.Index(key, "/")+1:], ProxypassSep)
		if len(proxyStr) == 4 {
			proxyStr = append(proxyStr, "/")
		} else if len(proxyStr) == 5 {
			proxyStr[4] = "/" + proxyStr[4]
		}

		ingress, err := c.extendIngressLister.ExtendIngresses(proxyStr[0]).Get(proxyStr[1])
		if err != nil {
			// The ExtendIngress resource may no longer exist, in which case we stop
			// processing.
			if errors.IsNotFound(err) {
				runtime.HandleError(fmt.Errorf("ExtendIngress %s/%s in work queue no longer exists", proxyStr[0], proxyStr[1]))
				return nil
			}
			return err
		}
		c.extendIngressCfg.updateEndpointsToCfg(proxyStr[2:], ingress, c.endpointLister)
	} else if strings.Contains(key, "Configmap") {
		splitStr = strings.Split(key, "/")
		configmap, err := c.configmapLister.ConfigMaps(splitStr[1]).Get(splitStr[2])
		if err != nil {
			c.extendIngressCfg.delCmFromCfg(splitStr[0])
		} else {
			c.extendIngressCfg.addOrUpdateCmToCfg(configmap, splitStr[0])
		}
	}

	if c.workqueue.Len() == 0 {
		time.Sleep(time.Second)
		if c.workqueue.Len() == 0 {
			c.extendIngressCfg.rebuildNginxConf()
			c.extendIngressCfg.startOrReloadNginx()
			c.extendIngressCfg.Start = false
		}
	}
	glog.V(4).Info(c.extendIngressCfg.jsonString())
	return nil
}
