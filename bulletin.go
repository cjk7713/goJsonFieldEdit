package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rakyll/coop"
)

type serviceStatusT struct {
	URL    string
	Status string
}

// serviceT 注册的服务
type serviceT struct {
	Services map[string]serviceStatusT
}

// SecureMap 并发安全的map
type SecureMap struct {
	m map[string]serviceStatusT
	sync.Mutex
}

var logger *log.Logger
var appRoot string
var dbFilePath string

const listTpl = `<!DOCTYPE html>
<html>
	<head>
		<meta charset="UTF-8">
		<title>服务列表</title>
		<style type="text/css">
			table {
				border-style: solid;
				border-width: 1px;
				border-collapse: collapse;
			}
			td, th {
				border-style: solid;
				border-width: 1px;
				border-color: #BFE0EC;
				padding: 0.2em 0.5em;
			}
			th {
				background-color: #E5F2F7;
				color: #205F8E;
			}
			.status-on {
				color: green;
			}
			.status-off {
				color: red;
			}
		</style>
	</head>
	<body>
	<h3>服务列表</h3>
	<table>
		<thead>
			<tr>
				<th>服务名称</th>
				<th>服务地址</th>
				<th>服务状态</th>
			</tr>
		</thead>
		<tbody>
			{{range $k, $v := .Services}}
			<tr>
				<td>{{$k}}</td>
				<td><a href="{{$v.URL}}" target="_blank">{{$v.URL}}</a></td>
				<td class="status-{{$v.Status}}">{{$v.Status}}</td>
			</tr>
			{{end}}
		</tbody>
	</table>
	</body>
</html>`

var listTplVar *template.Template

var registeredService *SecureMap

func (m *SecureMap) setURL(key string, url string) {
	m.Lock()
	defer m.Unlock()
	v, ok := m.m[key]
	if ok {
		v.URL = url
	} else {
		v = serviceStatusT{url, "off"}
	}
	m.m[key] = v
	// 同时写文件保存
	saveRegisterService()
}

func (m *SecureMap) setStatus(key string, status string) {
	m.Lock()
	defer m.Unlock()
	v, ok := m.m[key]
	if !ok {
		return
	}
	v.Status = status
	m.m[key] = v
	// 同时写文件保存
	saveRegisterService()
}

func (m *SecureMap) del(key string) bool {
	m.Lock()
	defer m.Unlock()
	_, exist := m.m[key]
	if exist == false {
		return false
	}
	delete(m.m, key)
	//
	saveRegisterService()
	return true
}

func (m *SecureMap) getMap() map[string]serviceStatusT {
	return m.m
}

func saveRegisterService() {
	services := registeredService.getMap()
	content, err := json.MarshalIndent(services, "", "  ")
	if err != nil {
		logger.Println(err)
		return
	}
	fh, err := os.OpenFile(dbFilePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		logger.Println(err)
		return
	}
	defer fh.Close()
	_, err = fh.Write(content)
	if err != nil {
		logger.Println(err)
	}
	return
}

func loadRegisterService() (services map[string]serviceStatusT) {
	content, err := ioutil.ReadFile(dbFilePath)
	if err != nil {
		logger.Println(err)
		return
	}
	err = json.Unmarshal(content, &services)
	if err != nil {
		logger.Println(err)
	}
	return
}

// 日志初始化函数
func getLogger() (logger *log.Logger) {
	os.Mkdir(filepath.Join(appRoot, "log"), 0755)
	logFile, err := os.OpenFile(filepath.Join(appRoot, "log/bulletin.log"), os.O_CREATE|os.O_RDWR|os.O_APPEND, 0755)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	logger = log.New(logFile, "\n", log.Ldate|log.Ltime|log.Lshortfile)
	return
}

func addService(resp http.ResponseWriter, req *http.Request) {
	query := req.URL.Query()
	serviceName, okName := query["name"]
	serviceURL, okURL := query["url"]
	if okName == false || okURL == false {
		resp.WriteHeader(http.StatusBadRequest)
		io.WriteString(resp, "{\"status\": \"false\", \"msg\": \"wrong query string\"}")
		return
	}
	tURL, err := url.QueryUnescape(serviceURL[0])
	if err != nil {
		resp.WriteHeader(http.StatusBadRequest)
		io.WriteString(resp, "{\"status\": \"false\", \"msg\": \"url参数值不合法\"}")
		return
	}
	registeredService.setURL(serviceName[0], tURL)

	resp.WriteHeader(http.StatusOK)
	io.WriteString(resp, "{\"status\": \"true\"}")
	return
}

func delService(resp http.ResponseWriter, req *http.Request) {
	query := req.URL.Query()
	serviceName, ok := query["name"]
	if ok == false {
		resp.WriteHeader(http.StatusBadRequest)
		io.WriteString(resp, "{\"status\": \"false\", \"msg\": \"Bad Request\"}")
		return
	}
	success := registeredService.del(serviceName[0])

	resp.WriteHeader(http.StatusOK)
	if success {
		io.WriteString(resp, "{\"status\": \"true\"}")
	} else {
		io.WriteString(resp, "{\"status\": \"false\"}")
	}
	return
}

func showList(resp http.ResponseWriter, req *http.Request) {
	services := registeredService.getMap()
	resp.WriteHeader(http.StatusOK)
	err := listTplVar.Execute(resp, serviceT{Services: services})
	if err != nil {
		logger.Printf("Execute template: %s", err)
	}
	return
}

func _checkStatus(url string) string {
	hc := &http.Client{
		Timeout: 5 * time.Second,
	}
	resp, err := hc.Get(url)
	if err != nil {
		log.Println("ERROR:", err)
		return "off"
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Println("ERROR:", fmt.Errorf("非正常响应，响应码为：%d", resp.StatusCode))
		return "off"
	}
	return "on"
}

func serviceStatusChecker() {
	coop.Every(5*time.Second, func() {
		for k, v := range registeredService.getMap() {
			registeredService.setStatus(k, _checkStatus(v.URL))
		}
	})
}

func main() {
	appRoot, _ = os.Getwd()
	dbFilePath = filepath.Join(appRoot, "services.json")
	port := flag.String("p", "8087", "port to run the web server")
	logger = getLogger()

	var err error
	services := loadRegisterService()
	if services == nil {
		services = make(map[string]serviceStatusT)
	}
	registeredService = &SecureMap{m: services}
	listTplVar, err = template.New("list-service").Parse(listTpl)
	if err != nil {
		logger.Fatalln("Parse list service template: ", err)
	}

	//
	go serviceStatusChecker()

	http.HandleFunc("/add", addService)
	http.HandleFunc("/del", delService)
	http.HandleFunc("/", showList)

	err = http.ListenAndServe(":"+*port, nil)
	if err != nil {
		logger.Fatalln("ListenAndServe: ", err)
	}
}
