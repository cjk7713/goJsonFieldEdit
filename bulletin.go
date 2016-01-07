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
	"os"
	"path/filepath"
	"sync"
)

type ServiceStruct struct {
	Services map[string]string
}
type SecureMap struct {
	m map[string]string
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
		</style>
	</head>
	<body>
	<h3>服务列表</h3>
	<table>
		<thead>
			<tr>
				<th>服务名称</th>
				<th>服务地址</th>
			</tr>
		</thead>
		<tbody>
			{{range $k, $v := .Services}}
			<tr>
				<td>{{$k}}</td>
				<td><a href="{{$v}}" target="_blank">{{$v}}</a>
			</tr>
			{{end}}
		</tbody>
	</table>
	</body>
</html>`

var listTplVar *template.Template

var registeredService *SecureMap

func (m *SecureMap) set(key string, value string) {
	m.Lock()
	defer m.Unlock()
	m.m[key] = value
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

func (m *SecureMap) getMap() map[string]string {
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
	_, err = fh.Write(content)
	if err != nil {
		logger.Println(err)
	}
	return
}

func loadRegisterService() (services map[string]string) {
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
	registeredService.set(serviceName[0], serviceURL[0])

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
	err := listTplVar.Execute(resp, ServiceStruct{Services: services})
	if err != nil {
		logger.Printf("Execute template: %s", err)
	}
	return
}

func main() {
	appRoot, _ = os.Getwd()
	dbFilePath = filepath.Join(appRoot, "services.json")
	port := flag.String("p", "8087", "port to run the web server")
	logger = getLogger()

	var err error
	services := loadRegisterService()
	if services == nil {
		services = make(map[string]string)
	}
	registeredService = &SecureMap{m: services}
	listTplVar, err = template.New("list-service").Parse(listTpl)
	if err != nil {
		logger.Fatalln("Parse list service template: ", err)
	}

	http.HandleFunc("/add/", addService)
	http.HandleFunc("/del/", delService)
	http.HandleFunc("/", showList)

	err = http.ListenAndServe(":"+*port, nil)
	if err != nil {
		logger.Fatalln("ListenAndServe: ", err)
	}
}
