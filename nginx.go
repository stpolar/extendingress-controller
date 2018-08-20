package main

import (
	"errors"
	"html/template"
	"io/ioutil"
	"log"
	"os"
	"os/exec"

	"runtime"
	"strings"

	"github.com/golang/glog"
	"bufio"
)

const (
	NginxExe               = "C:\\Program Files\\nginx-1.14.0\\nginx.exe"
	WorkerProcesses        = "2"
	WorkerRlimitNofile     = "65535"
	EventUse               = "epoll"
	EventWorkerConnections = "65535"
	HttpSendfile           = "on"
	TcpNopush              = "on"
	TcpNodelay             = "on"
	ProxypassSep           = "_"
)

var (
	NginxStart    = "/usr/sbin/nginx -c /etc/nginx/nginx.conf"
	NginxReload   = "/usr/sbin/nginx -s reload"
	NginxConfFile = "/etc/nginx/nginx.conf"
	NginxTestFile = "/etc/nginx/nginx-test.conf"
	NginxCheck    = "/usr/sbin/nginx -t -c " + NginxTestFile
	NginxTmpPath  = "/etc/nginx/template/nginx.tmpl"
)

func RunCmd(truecmd string) (string, error) {
	var cmd *exec.Cmd
	if !(runtime.GOOS == "windows") {
		cmd = exec.Command("/bin/bash", "-c", truecmd)
	} else {
		splitStr := strings.Split(truecmd[strings.Index(truecmd, "nginx.exe")+10:], " ")
		cmd = exec.Command(NginxExe, splitStr[0:]...)
	}

	stdout, err := cmd.StdoutPipe()
	defer stdout.Close()
	if err != nil {
		log.Println("runCmd: stdoutPipe: " + err.Error())
		return "", err
	}

	stderr, err := cmd.StderrPipe()
	defer stderr.Close()
	if err != nil {
		log.Println("runCmd: stderrPipe: ", err.Error())
		return "", err
	}

	if err := cmd.Start(); err != nil {
		log.Println("runCmd: start: ", err.Error())
		return "", err
	}

	go func() {
		cmd.Wait()
	}()

	var stderrStr string
	r := bufio.NewReader(stderr)
	for {
		line, _, err := r.ReadLine()
		if err != nil{
			break
		}
		stderrStr += string(line) + "\n"
	}
	if len(stderrStr) > 0 {
		return stderrStr, errors.New(string(stderrStr))
	}

	var stdoutStr string
	r = bufio.NewReader(stderr)
	for {
		line, _, err := r.ReadLine()
		if err != nil{
			break
		}
		stdoutStr += string(line) + "\n"
	}
	if len(stdoutStr) > 0 {
		return stdoutStr, nil
	}

	return "", nil
}

func readFile(path string) (string, error) {
	fi, err := os.Open(path)
	if err != nil {
		panic(err)
	}
	defer fi.Close()
	fd, err := ioutil.ReadAll(fi)
	return string(fd), err
}

func checkIngressValid(cfg ExtendIngressCfg, servers []*Server, upstreams []*Upstream) (string, error) {
	for _, serVal := range servers {
		cfg.Servers[serVal.ServerName] = serVal
	}
	for _, upVal := range upstreams {
		cfg.Upstreams[getProxyPass(upVal.Namespace, upVal.IngressName, upVal.Name, upVal.Port, upVal.Path)] = upVal
	}
	err := rebuidNginxConf(&cfg, NginxTestFile)
	if err != nil {
		return "", err
	}
	runStr, err := RunCmd(NginxCheck)
	if err != nil {
		//glog.Error(err)
		return "", err
	}
	return runStr, nil
}

func rebuidNginxConf(cfg *ExtendIngressCfg, filePath string) error {
	str, err := readFile(NginxTmpPath)
	if err != nil {
		glog.Error(err)
	}
	t := template.New("nginx conf rebuid")
	t, _ = t.Parse(str)

	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	defer file.Close()
	if err != nil {
		glog.Error(err)
		return err
	}
	err = t.Execute(file, cfg)
	if err != nil {
		glog.Error(err)
	}

	return nil
}

func startNginx() {
	log.Println(NginxStart)
	_, err := RunCmd(NginxStart)
	log.Println(NginxStart)
	if err != nil {
		glog.Error(err.Error())
	}
}

func reloadNginx() {
	_, err := RunCmd(NginxReload)
	if err != nil {
		glog.Error(err.Error())
	}
}

func getProxyPass(namesapce, ingressName, epName, port, location string) string {
	if location != "/" {
		return namesapce + ProxypassSep + ingressName + ProxypassSep + epName + ProxypassSep + port + ProxypassSep + location[1:]
	} else {
		return namesapce + ProxypassSep + ingressName + ProxypassSep + epName + ProxypassSep + port
	}
}

func resetNginxComByOS() {
	if runtime.GOOS == "windows" {
		NginxConfFile = "./nginx.conf"
		NginxTestFile = "./nginx-test.conf"
		NginxTmpPath = "./nginx.tmpl"
		NginxStart = NginxExe + " -c ./nginx.conf"
		NginxReload = NginxExe + " -s reload"
		NginxCheck = NginxExe + " -t -c ./nginx-test.conf"
	} else {
		NginxStart = "/usr/sbin/nginx -c /etc/nginx/nginx.conf"
		NginxReload = "/usr/sbin/nginx -s reload"
		NginxConfFile = "/etc/nginx/nginx.conf"
		NginxTestFile = "/etc/nginx/nginx-test.conf"
		NginxCheck = "/usr/sbin/nginx -t -c " + NginxTestFile
		NginxTmpPath = "/etc/nginx/template/nginx.tmpl"
	}
}
