# extendIngress

## Description

k8s custom resource

extendIngress是对nginx server的个性化配置，本项目实现了k8s下的Nginx自定义配置，将监听k8s的自定义资源extendIngress，并根据配置自动刷新到Nginx，最后重启Nginx。将外部流量导入到K8s集群内部的具体pod中。

项目总体架构图如下：

![](/assets/controller.png)

关于项目的更多细节可以查看以下几篇博客：

[谈谈k8s的leader选举--分布式资源锁](https://blog.csdn.net/weixin_39961559/article/details/81877056)

[理解kubernetes tools/cache包: part 1](https://blog.csdn.net/weixin_39961559/article/details/81938716)

[理解kubernetes’ tools/cache包: part 2](https://blog.csdn.net/weixin_39961559/article/details/81940918)

[理解Kubernetes’ tools/cache包: part 3](https://blog.csdn.net/weixin_39961559/article/details/81945559)

[理解kubernetes’ tools/cache包: part 4](https://blog.csdn.net/weixin_39961559/article/details/81946398)

[理解kubernetes’ tools/cache包: part 5](https://blog.csdn.net/weixin_39961559/article/details/81946899)

[理解kubernetes’ tools/cache包: part 6](https://blog.csdn.net/weixin_39961559/article/details/81948239)

[理解kubernetes’ tools/cache包: part 7](https://blog.csdn.net/weixin_39961559/article/details/81948541)

## 如何使用

* 使用curl进行增删查改：

add:  

```
curl -v  --header "Content-Type: application/json" \
  --request POST \
  --data \
'{
    "apiVersion": "extendingresscontroller.k8s.io/v1alpha1",
    "kind": "ExtendIngress",
    "metadata": {
        "name": "foos",
        "namespace": "ingress-nginx"
    },
    "spec": {
        "rules": [
            {
                "host": "www.foos.com",
                "http": {
                    "paths": [
                        {
                            "backend": {
                                "serviceName": "productpage",
                                "servicePort": 9080
                            },
                            "locationParam": {
                                access_log: "false",
                                cors: "true",
                                proxy_set_header: Host       $proxy_host,Connection close,X-real-ip $remote_addr,X-Forwarded-For $proxy_add_x_forwarded_for
                            },
                            "path": "/foo",
                            "upstreamParam": {
                                "lbalg": "least_conn"
                            }
                        }
                    ]
                }
            }
        ]
    }
}' \

https://localhost:6443/apis/extendingresscontroller.k8s.io/v1alpha1/namespaces/ingress-nginx/extendingresses/foos
```

update:

```
 curl -v --header "Content-Type: application/json" \
   --request PUT\
  --data \
'{
    "apiVersion": "extendingresscontroller.k8s.io/v1alpha1",
    "kind": "ExtendIngress",
    "metadata": {
        "name": "foos",
        "namespace": "ingress-nginx"
    },
    "spec": {
        "rules": [
            {
                "host": "www.foos.com",
                "http": {
                    "paths": [
                        {
                            "backend": {
                                "serviceName": "productpage",
                                "servicePort": 9080
                            },
                            "locationParam": {
                                access_log: "false",
                                cors: "true",
                                proxy_set_header: Host       $proxy_host,Connection close,X-real-ip $remote_addr,X-Forwarded-For $proxy_add_x_forwarded_for
                            },
                            "path": "/foo",
                            "upstreamParam": {
                                "lbalg": "least_conn"
                            }
                        }
                    ]
                }
            }
        ]
    }
}' \

https://localhost:6443/apis/extendingresscontroller.k8s.io/v1alpha1/namespaces/ingress-nginx/extendingresses/foos
```

delete:

```
curl -v -X DELETE https://localhost:6443/apis/extendingresscontroller.k8s.io/v1alpha1/namespaces/ingress-nginx/extendingresses/foos
```

get:

```
curl -v https://localhost:6443/apis/extendingresscontroller.k8s.io/v1alpha1/namespaces/ingress-nginx/extendingresses/foos
or for all
curl -v https://localhost:6443/apis/extendingresscontroller.k8s.io/v1alpha1/namespaces/{namespace}

```

* 使用kubectl客户端增删查该
* 使用其它http client等

对于配置项分为common、event、http、location和upstream块，其中common、event和http的配置是全局有效，以configmap为载体，location和upstream块是单个server有效，具体为每个extendIngress对象，以具体的nginx配置为例![](/assets/nginx-template.png)在k8s里是怎么使用到上面的这些参数配置的，在extendIngress程序的启动参数配置了configmap参数--commonConf、--eventConf、--httpConf分别代表common、event和http对应的configmap，将配置配在configmap。location和upstream的yaml示例如下：

```
apiVersion: extendingresscontroller.k8s.io/v1alpha1
kind: ExtendIngress
metadata:
  name: tomcat1
  namespace: ingress-nginx
spec:
  rules:
  - host: www.test23.vom
    http:
      paths:
      - backend:
          serviceName: tomcat
          servicePort: 8080
        path: /
        upstreamParam:
          "lbalg": "least_conn"
        locationParam:
          access_log: "false"
          cors: "true"
          proxy_set_header: Host       $proxy_host,Connection close,X-real-ip $remote_addr,X-Forwarded-For $proxy_add_x_forwarded_for 
```

\*跨域需要在locationParam中配置key为cors，value为true，对于同名key，value合并在一起，以","分隔。

## 待开发

本extendIngress目前的配置项只支持key-value形式，对于块状配置还不支持



