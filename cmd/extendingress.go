/*
author: 	polarwu
date:		2018/7/23
purpose:	watch the extendingress resource and endpoint resource, rewrite the
			nginx.conf and reload nginx.
*/

package cmd

import (
	"fmt"
	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/api/errors"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/extendingress-controller/pkg/apis/extendingresscontroller/v1alpha1"

	"encoding/json"
	"k8s.io/api/core/v1"
	"log"
	"strings"
	"runtime"
)

type ExtendIngressCfg struct {
	CommonConf map[string]string    //config global param ,cpu etc.
	HttpConf   map[string]string    //config http field global param
	EventConf  map[string]string    //config event field global param
	Servers    map[string]*Server   //the key name by domain
	Upstreams  map[string]*Upstream //the key name by services' namespace-name-port
	Reload     bool                 //if true, should reload nginx,else none
	Start      bool
	IngressEp  map[string][]string
	Config     Configuration
}

type Server struct {
	ServerName string               //domain
	Locations  map[string]*Location //location array
	Conf       map[string]string    //server param in nginx
	Listen     int                  //listen
}

type Upstream struct {
	Namespace   string                 //ep namespace
	IngressName string                 //Ingress name
	Name        string                 //ep name
	Port        string                 //port
	Path        string                 //path
	Endpoints   []string               //endpoint array
	Conf        map[string]interface{} //upstream param in nginx
}

type Location struct {
	Pass      string                 //location pass
	ProxyPass string                 //proxy addr
	Cors      bool                   //cors
	Conf      map[string]interface{} //location param in nginx
}

type Configuration struct {
	ApiserverHost  *string
	CommonConf     *string
	EventConf      *string
	HttpConf       *string
	KubeConfigFile *string
	DefHealthzURL  *string
	Version        *bool
	ListenPort     ListenPorts
}

type ListenPorts struct {
	HTTP   int
	HTTPS  int
	Status int
}

func (e *ExtendIngressCfg) parseExtendIngressIntoCfg(ingress *v1alpha1.ExtendIngress, epList corelisters.EndpointsLister) ([]*Server, []*Upstream, string, error) {
	//var server *Server
	servers, upstreams := e.extendingressToServer(ingress, epList)
	// check the servers and upstreams parse by ingress is valid.
	cfg := e.returnBaseCfg()
	nginxTestStr, err := checkIngressValid(cfg, servers, upstreams)
	return servers, upstreams, nginxTestStr, err
}

func (e *ExtendIngressCfg) extendingressToServer(ingress *v1alpha1.ExtendIngress, endpointList corelisters.EndpointsLister) ([]*Server, []*Upstream) {
	var servers []*Server
	var upsteams []*Upstream
	for _, rule := range ingress.Spec.Rules {
		server := &Server{
			Locations: make(map[string]*Location),
			Conf:      make(map[string]string),
		}
		addServerFlag := false
		for _, path := range rule.HTTP.Paths {
			ep, err := endpointList.Endpoints(ingress.Namespace).Get(path.Backend.ServiceName)
			if err != nil {
				if errors.IsNotFound(err) {
					glog.Error(fmt.Sprintf("ep %s not in namespace %s", ingress.Namespace, path.Backend.ServiceName))
				}
			}
			addEndpointFlag := false
			var epSubsetIP []string
			if ep != nil {
				if ep.Subsets != nil {
					for _, subset := range ep.Subsets {
						if subset.Addresses != nil {
							for _, address := range subset.Addresses {
								addEndpointFlag = true
								epSubsetIP = append(epSubsetIP, address.IP)
							}
						}
					}
				}
			}
			var proxyPass string
			if len(epSubsetIP) > 0 {
				proxyPass = getProxyPass(ingress.Namespace, ingress.Name, path.Backend.ServiceName, path.Backend.ServicePort.String(), path.Path)
			} else {
				proxyPass = "503"
			}
			addServerFlag = true
			locationConf := make(map[string]interface{})
			for lkey, lval := range path.LocationParam {
				locationConf[lkey] = lval
			}
			var cors bool
			if corsflag, ok := locationConf["cors"]; ok {
				if flag, ok := corsflag.(bool); ok {
					cors = flag
				} else {
					if corsflag == "true" {
						cors = true
					}
				}
				delete(locationConf, "cors")
			}
			location := &Location{
				ProxyPass: proxyPass,
				Conf:      locationConf,
				Pass:      path.Path,
				Cors:      cors,
			}

			upstream := &Upstream{
				Namespace:   ingress.Namespace,
				IngressName: ingress.Name,
				Name:        path.Backend.ServiceName,
				Port:        path.Backend.ServicePort.String(),
				Path:        path.Path,
				Endpoints:   epSubsetIP,
				Conf:        path.UpstreamParam,
			}
			if !addEndpointFlag {
				server.Locations[path.Path] = location
				continue
			}
			server.Locations[path.Path] = location
			upsteams = append(upsteams, upstream)
		}
		if addServerFlag {
			server.ServerName = rule.Host
			server.Listen = e.Config.ListenPort.HTTP
			servers = append(servers, server)
		}
	}
	return servers, upsteams
}

func (e *ExtendIngressCfg) updateServerAndUpstreamToCfg(servers []*Server, upstreams []*Upstream, lastDomains map[string][]v1alpha1.ExtendHTTPIngressPath,
	namespace, ingressName string) {
	for _, serVal := range servers {
		var lastPaths []v1alpha1.ExtendHTTPIngressPath
		if lastDomain, ok := lastDomains[serVal.ServerName]; ok {
			lastPaths = lastDomain
			delete(lastDomains, serVal.ServerName)
		}

		if _, ok := e.Servers[serVal.ServerName]; ok {
			for locKey, locVal := range serVal.Locations {
				e.Servers[serVal.ServerName].Locations[locKey] = locVal

				for pos, lastPath := range lastPaths {
					if lastPath.Path == locVal.Pass {
						lastPaths = append(lastPaths[0:pos], lastPaths[pos+1:len(lastPaths)]...)
						delete(e.Upstreams, getProxyPass(namespace, ingressName, lastPath.Backend.ServiceName,
							lastPath.Backend.ServicePort.String(), lastPath.Path))
						break
					}
				}
			}
			if len(lastPaths) > 0 {
				for _, lastPath := range lastPaths {
					delete(e.Servers[serVal.ServerName].Locations, lastPath.Path)
					delete(e.Upstreams, getProxyPass(namespace, ingressName, lastPath.Backend.ServiceName,
						lastPath.Backend.ServicePort.String(), lastPath.Path))
				}
			}
		} else {
			e.Servers[serVal.ServerName] = serVal
		}
	}

	for lastSerKey, _ := range lastDomains {
		for _, location := range e.Servers[lastSerKey].Locations {
			delete(e.Upstreams, location.ProxyPass)
		}
		delete(e.Servers, lastSerKey)
	}

	for _, upstream := range upstreams {
		e.Upstreams[getProxyPass(upstream.Namespace, upstream.IngressName, upstream.Name, upstream.Port, upstream.Path)] = upstream
	}
}

func (e *ExtendIngressCfg) delServerAndUpstreamFromCfg(serverAndLocations []string) {
	for _, serverLoc := range serverAndLocations {
		splitStr := strings.Split(serverLoc, "-")
		for i := 1; i < len(splitStr); i++ {
			delete(e.Upstreams, e.Servers[splitStr[0]].Locations["/"+splitStr[i]].ProxyPass)
			delete(e.Servers[splitStr[0]].Locations, "/"+splitStr[i])
		}
		if len(e.Servers[splitStr[0]].Locations) == 0 {
			delete(e.Servers, splitStr[0])
		}
	}
}

func (e *ExtendIngressCfg) updateEndpointsToCfg(proxyStr []string, ingress *v1alpha1.ExtendIngress, endpointList corelisters.EndpointsLister) {
	//namespace, ingress.Name, name, path.Backend.ServicePort.String(), path.Path
	if ingress.Status.State == "unReady" {
		return
	}

	var epSubsetIP []string
	ep, err := endpointList.Endpoints(ingress.Namespace).Get(proxyStr[0])
	if err != nil {
		if errors.IsNotFound(err) {
			glog.Error(fmt.Sprintf("ep %s not in namespace %s", ingress.Namespace, proxyStr[0], proxyStr[1]))
		}
	}
	if ep != nil && ep.Subsets != nil {
		for _, subset := range ep.Subsets {
			if subset.Addresses != nil {
				for _, address := range subset.Addresses {
					epSubsetIP = append(epSubsetIP, address.IP)
				}
			}
		}
	}
	upstream := &Upstream{
		Namespace:   ingress.Namespace,
		IngressName: ingress.Name,
		Name:        proxyStr[0],
		Port:        proxyStr[1],
		Path:        proxyStr[2],
		Endpoints:   epSubsetIP,
		//Conf: path.UpstreamParam,
	}
	for _, rule := range ingress.Spec.Rules {
		for _, path := range rule.HTTP.Paths {
			if path.Backend.ServiceName == proxyStr[0] && path.Backend.ServicePort.String() == proxyStr[1] {
				if len(epSubsetIP) > 0 {
					if _, ok := e.Servers[rule.Host]; ok {
						if _, ok = e.Servers[rule.Host].Locations[proxyStr[2]]; ok {
							e.Servers[rule.Host].Locations[proxyStr[2]].ProxyPass = getProxyPass(ingress.Namespace, ingress.Name,
								proxyStr[0], proxyStr[1], proxyStr[2])
							upstream.Conf = path.UpstreamParam
						}
					}
				} else {
					e.Servers[rule.Host].Locations[proxyStr[2]].ProxyPass = "503"
				}
			}
		}
	}
	if len(epSubsetIP) > 0 {
		e.Upstreams[getProxyPass(ingress.Namespace, ingress.Name, proxyStr[0], proxyStr[1], proxyStr[2])] = upstream
	} else {
		delete(e.Upstreams, getProxyPass(ingress.Namespace, ingress.Name, proxyStr[0], proxyStr[1], proxyStr[2]))
	}
}

func (e *ExtendIngressCfg) rebuildNginxConf() {
	cfg := e.returnBaseCfg()
	if len(e.CommonConf) == 0 {
		e.CommonConf = cfg.CommonConf
	}
	if len(e.EventConf) == 0 {
		e.EventConf = cfg.EventConf
	}
	if len(e.HttpConf) == 0 {
		e.HttpConf = cfg.HttpConf
	}
	cfg.CommonConf = e.CommonConf
	cfg.EventConf = e.EventConf
	cfg.HttpConf = e.HttpConf

	_, err := checkIngressValid(cfg, []*Server{}, []*Upstream{})
	if err != nil {
		log.Println(err.Error())
		if !strings.Contains(err.Error(), "successful") {
			glog.Error(err.Error())
			return
		}
	}
	rebuidNginxConf(e, NginxConfFile)
}

func (e *ExtendIngressCfg) addOrUpdateCmToCfg(configmap *v1.ConfigMap, event string) {
	if strings.Contains(event, "-common") {
		e.CommonConf = configmap.Data
	} else if strings.Contains(event, "-event") {
		e.EventConf = configmap.Data
	} else if strings.Contains(event, "-http") {
		e.HttpConf = configmap.Data
	}
}

func (e *ExtendIngressCfg) delCmFromCfg(event string) {
	if strings.Contains(event, "-common") {
		e.CommonConf = make(map[string]string)
	} else if strings.Contains(event, "-event") {
		e.EventConf = make(map[string]string)
	} else if strings.Contains(event, "-http") {
		e.HttpConf = make(map[string]string)
	}
}

func (e *ExtendIngressCfg) startOrReloadNginx() {
	if e.Start {
		startNginx()
	} else {
		reloadNginx()
	}
}

func (e *ExtendIngressCfg) jsonString() string {
	jsons, errs := json.Marshal(e)
	if errs != nil {
		glog.Error(errs.Error())
	}
	return string(jsons)
}

func (e *ExtendIngressCfg) returnBaseCfg() ExtendIngressCfg {
	defHealthzUR := "/healthz"
	return ExtendIngressCfg{
		CommonConf: map[string]string{"worker_processes": WorkerProcesses, "worker_rlimit_nofile": WorkerRlimitNofile},
		HttpConf:   map[string]string{"sendfile": HttpSendfile, "tcp_nopush": TcpNopush, "tcp_nodelay": TcpNodelay},
		EventConf:  map[string]string{"worker_connections": EventWorkerConnections, func() string{
			if runtime.GOOS == "windows" {
				return "#use"
			}else {
				return "use"
			}
		}(): EventUse},
		Servers:    make(map[string]*Server),
		Upstreams:  make(map[string]*Upstream),
		Config: Configuration{
			DefHealthzURL: &defHealthzUR,
			ListenPort: ListenPorts{
				HTTP:   8138,
				HTTPS:  8443,
				Status: 18080,
			},
		},
	}
}