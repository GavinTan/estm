package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/elastic/go-elasticsearch/v7"
	"github.com/elastic/go-elasticsearch/v7/esutil"
	"github.com/gin-gonic/gin"
	"github.com/robfig/cron"
)

//go:embed templates
var FS embed.FS

var (
	configName = "config.toml"
	logger     = log.Default()

	indexName         string
	taskActions       string
	monitorEnvList    []string
	writeDataEs       []string
	writeDataEsUser   string
	writeDataEsPasswd string
	es                *elasticsearch.Client
)

type Config struct {
	IndexName         string   `toml:"index_name"`
	TaskActions       string   `toml:"task_actions"`
	MonitorEnvs       []string `toml:"monitor_envs"`
	WriteDataEs       []string `toml:"write_data_es"`
	WriteDataEsUser   string   `toml:"write_data_es_user"`
	WriteDataEsPasswd string   `toml:"write_data_es_password"`
}

type taskData struct {
	Node               string `json:"node"`
	Id                 string `json:"id"`
	Type               string `json:"type"`
	Action             string `json:"action"`
	Description        string `json:"description"`
	StartTimeInMillis  int    `json:"start_time_in_millis"`
	RunningTimeInNanos int    `json:"running_time_in_nanos"`
	Cancellable        bool   `json:"cancellable"`
	ParentTaskId       string `json:"parent_task_id"`
	Index              string `json:"index"`
	CreateAt           string `json:"created_at"`
	Cluster            string `json:"cluster"`
}

type nodeData struct {
	Name  string
	Host  string
	Tasks map[string]taskData
}

type retTaskData struct {
	Node        string `json:"node"`
	Id          string `json:"id"`
	Type        string `json:"type"`
	Action      string `json:"action"`
	Dsl         string `json:"dsl"`
	StartTime   string `json:"start_time"`
	RunningTime string `json:"running_time"`
	Cancellable bool   `json:"cancellable"`
	Index       string `json:"index"`
}

type checkSrvData struct {
	Host string `json:"host"`
	Port string `json:"port"`
}

type esData struct {
	Host    string `json:"host"`
	Port    string `json:"port"`
	Systemd string `json:"systemd"`
	Sid     int64  `json:"sid"`
	Remark  string `json:"remark"`
}

type serviceData struct {
	Name     string `json:"name,omitempty"`
	Children []struct {
		Name   string   `json:"name,omitempty"`
		Data   []esData `json:"data"`
		Cid    int64    `json:"cid"`
		Kibana string   `json:"kibana,omitempty"`
		User   string   `json:"user,omitempty"`
		Passwd string   `json:"passwd,omitempty"`
	} `json:"children"`
	CreatedAt int64 `json:"createdAt,omitempty"`
	UpdatedAt int64 `json:"updatedAt,omitempty"`
}

type servceObj struct {
	es        *elasticsearch.Client
	indexName string
}

type esUrlData struct {
	Servers []string
	User    string
	Passwd  string
}

func NewRequest(method string, url string, body io.Reader, auth ...string) (resp *http.Response, err error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}

	if len(auth) == 2 {
		req.SetBasicAuth(auth[0], auth[1])
	}

	return client.Do(req)
}

func getHistoryData(args map[string]string) interface{} {
	var buf bytes.Buffer

	query := map[string]interface{}{
		"query": map[string]interface{}{
			"bool": map[string]interface{}{
				"must": []interface{}{},
				"must_not": map[string]interface{}{
					"match": map[string]interface{}{
						"action.keyword": "indices:data/read/scroll",
					},
				},
			},
		},
		"sort": []interface{}{
			map[string]interface{}{
				"start_time_in_millis": map[string]interface{}{
					"order": "desc",
				},
			},
		},
		"from":             args["from"],
		"size":             args["size"],
		"track_total_hits": true,
	}

	if args["qn"] != "" {
		query["query"].(map[string]interface{})["bool"].(map[string]interface{})["must"] = append(
			query["query"].(map[string]interface{})["bool"].(map[string]interface{})["must"].([]interface{}),
			map[string]interface{}{
				"match": map[string]interface{}{
					"node": args["qn"],
				},
			},
		)
	}

	if args["qc"] != "" {
		query["query"].(map[string]interface{})["bool"].(map[string]interface{})["must"] = append(
			query["query"].(map[string]interface{})["bool"].(map[string]interface{})["must"].([]interface{}),
			map[string]interface{}{
				"match": map[string]interface{}{
					"cluster.keyword": args["qc"],
				},
			},
		)
	}

	if args["q"] != "" {
		query["query"].(map[string]interface{})["bool"].(map[string]interface{})["should"] = []interface{}{
			map[string]interface{}{
				"multi_match": map[string]interface{}{
					"query":  args["q"],
					"fields": []string{"index", "_id"},
				},
			},
		}

		query["query"].(map[string]interface{})["bool"].(map[string]interface{})["minimum_should_match"] = 1
	}

	if args["qt"] != "" {
		qtSlice := strings.Split(args["qt"], ",")

		query["query"].(map[string]interface{})["bool"].(map[string]interface{})["filter"] = map[string]interface{}{
			"range": map[string]interface{}{
				"start_time_in_millis": map[string]interface{}{
					"gte": qtSlice[0],
					"lte": qtSlice[1],
				},
			},
		}
	}

	if args["sortOrder"] != "" {
		query["sort"] = []interface{}{
			map[string]interface{}{
				args["sortField"]: map[string]interface{}{
					"order": args["sortOrder"],
				},
			},
		}
	}

	if err := json.NewEncoder(&buf).Encode(query); err != nil {
		fmt.Println(err)
	}

	res, err := es.Search(es.Search.WithIndex(fmt.Sprintf("estasklog-%s", args["qd"])), es.Search.WithBody(&buf))
	if err != nil || res.StatusCode != 200 {
		fmt.Println(err)
		return gin.H{"total": 0, "data": []interface{}{}}
	}

	var r map[string]interface{}
	json.NewDecoder(res.Body).Decode(&r)

	return gin.H{"total": r["hits"].(map[string]interface{})["total"].(map[string]interface{})["value"], "data": r["hits"].(map[string]interface{})["hits"].([]interface{})}
}

func getRealData(ed esUrlData) interface{} {
	server := getServer(ed.Servers)
	if server == "" {
		logger.Printf("%s no server is available\n", ed.Servers)
		return []retTaskData{}
	}

	resp, err := NewRequest("GET", fmt.Sprintf("%s/_tasks?actions=*search&detailed", server), http.NoBody, ed.User, ed.Passwd)
	if err != nil {
		return []retTaskData{}
	}

	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		fmt.Println(string(data))
		return []retTaskData{}
	}

	var newData map[string]map[string]nodeData
	json.Unmarshal(data, &newData)

	var retData retTaskData
	tmpData := []retTaskData{}

	for _, v := range newData["nodes"] {
		for kk, vv := range v.Tasks {

			reIndex := regexp.MustCompile(`indices\[(.*?)\]`)
			findIndex := reIndex.FindStringSubmatch(vv.Description)
			if len(findIndex) == 2 {
				retData.Index = findIndex[1]
			}

			reDsl := regexp.MustCompile(`source\[(.*?)\]$`)
			findDsl := reDsl.FindStringSubmatch(vv.Description)
			if len(findDsl) == 2 {
				retData.Dsl = findDsl[1]
			}

			retData.Id = kk
			retData.Type = vv.Type
			retData.Action = vv.Action
			retData.Node = v.Host
			retData.StartTime = time.Unix(int64(vv.StartTimeInMillis)/1000, 0).Format("2006-01-02 15:04:05")
			retData.RunningTime = fmt.Sprintf("%.9f", float64(vv.RunningTimeInNanos)/float64(1e9))
			retData.Cancellable = vv.Cancellable

			if vv.ParentTaskId == "" {
				tmpData = append(tmpData, retData)
			}
		}
	}

	return tmpData
}

func cancelTask(tid string, servers []string, auth ...string) bool {
	server := getServer(servers)
	if server == "" {
		logger.Printf("%s no server is available\n", servers)
		return false
	}
	resp, err := NewRequest("POST", fmt.Sprintf("%s/_tasks/%s/_cancel", server, tid), http.NoBody, auth...)
	if err != nil {
		fmt.Println(err)
		return false
	}

	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || strings.Contains(string(body), "node_failures") {
		return false
	}

	return true
}

func Service() *servceObj {
	return &servceObj{es: es, indexName: "estm-service"}
}

func (sd servceObj) All() interface{} {
	var buf bytes.Buffer

	res, err := sd.es.Search(sd.es.Search.WithIndex(sd.indexName), sd.es.Search.WithBody(&buf), sd.es.Search.WithSort("createdAt:asc"))
	if err != nil || res.StatusCode != 200 {
		return []interface{}{}
	}

	var r map[string]interface{}
	json.NewDecoder(res.Body).Decode(&r)
	return r["hits"].(map[string]interface{})["hits"].([]interface{})
}

func (sd servceObj) Get(id string) interface{} {
	// var buf bytes.Buffer

	res, err := sd.es.Get(sd.indexName, id)
	if err != nil || res.StatusCode != 200 {
		return []interface{}{}
	}

	var r map[string]interface{}
	json.NewDecoder(res.Body).Decode(&r)

	return r["_source"]
}

func (sd servceObj) Create(data serviceData) error {
	data.CreatedAt = time.Now().UnixNano() / 1e6
	res, err := sd.es.Index(sd.indexName, esutil.NewJSONReader(data), sd.es.Index.WithRefresh("true"))

	if err != nil {
		return err
	}

	defer res.Body.Close()

	if res.IsError() {
		body, _ := io.ReadAll(res.Body)
		return fmt.Errorf(string(body))
	}

	return nil
}

func (sd servceObj) Update(id string, data serviceData) error {
	data.UpdatedAt = time.Now().UnixNano() / 1e6
	res, err := sd.es.Update(sd.indexName, id, esutil.NewJSONReader(map[string]serviceData{"doc": data}), sd.es.Update.WithRefresh("true"))

	if err != nil {
		return err
	}

	defer res.Body.Close()

	if res.IsError() {
		body, _ := io.ReadAll(res.Body)
		return fmt.Errorf(string(body))
	}

	return nil
}

func (sd servceObj) Delete(id string) error {
	res, err := sd.es.Delete(sd.indexName, id, sd.es.Delete.WithRefresh("true"))
	if err != nil {
		return err
	}

	defer res.Body.Close()

	if res.IsError() {
		body, _ := io.ReadAll(res.Body)
		return fmt.Errorf(string(body))
	}

	return nil
}

func checkEsService(data checkSrvData) int {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(data.Host, data.Port), time.Second*10)
	if err != nil {
		return 0
	}
	defer conn.Close()

	return 1
}

func writeTaskData(cluster string, data map[string]map[string]nodeData) {
	indexName := fmt.Sprintf("%s-%s", indexName, time.Now().Format("2006-01-02"))
	for _, node := range data["nodes"] {
		for taskId, task := range node.Tasks {
			reIndex := regexp.MustCompile(`indices\[(.*?)\]`)
			findIndex := reIndex.FindStringSubmatch(task.Description)
			if len(findIndex) == 2 {
				task.Index = findIndex[1]
			}

			task.CreateAt = time.Now().Format("2006-01-02 15:04:05")
			task.Node = node.Host
			task.Cluster = cluster

			if task.ParentTaskId == "" {
				res, err := es.Index(indexName, esutil.NewJSONReader(task), es.Index.WithDocumentID(taskId))
				// res, err := es.Index(indexName, esutil.NewJSONReader(vv))

				if err != nil {
					logger.Println(err)
					return
				}

				defer res.Body.Close()
				// io.Copy(io.Discard, res.Body)

				if res.IsError() {
					body, _ := io.ReadAll(res.Body)
					logger.Printf("write index failed! %s\n", string(body))
				}
			}

		}
	}
}

func getTaskLog(cluster string, servers []string, auth ...string) {
	server := getServer(servers)
	if server == "" {
		logger.Printf("%s no server is available\n", servers)
		return
	}

	resp, err := NewRequest("GET", fmt.Sprintf("%s/_tasks?actions=%s&detailed", server, taskActions), http.NoBody, auth...)
	if err != nil {
		logger.Println(err)
		return
	}

	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var data map[string]map[string]nodeData
	json.Unmarshal(body, &data)

	writeTaskData(cluster, data)
}

func retryFailedShard(servers []string, noCheck bool, auth ...string) {
	server := getServer(servers)
	if server == "" {
		logger.Printf("%s no server is available\n", servers)
		return
	}

	var clusterState struct {
		ClusterName      string `json:"cluster_name"`
		NumberOfNodes    int    `json:"number_of_nodes"`
		Status           string `json:"status"`
		UnassignedShards int    `json:"unassigned_shards"`
	}

	resp, err := NewRequest("GET", fmt.Sprintf("%s/_cluster/health", server), http.NoBody, auth...)
	if err != nil {
		logger.Println(err)
		return
	}

	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	json.Unmarshal(body, &clusterState)

	if noCheck || clusterState.Status == "red" {
		logger.Println(server, clusterState.Status)
		go func() {
			resp, err := NewRequest("POST", fmt.Sprintf("%s/_cluster/reroute?retry_failed=true&metric=none", server), http.NoBody, auth...)
			if err != nil {
				logger.Println(err)
				return
			}

			defer resp.Body.Close()

			io.Copy(io.Discard, resp.Body)
		}()
	}
}

func runRetryFailedShard() {
	for _, service := range Service().All().([]interface{}) {
		d, _ := json.Marshal(service.(map[string]interface{})["_source"])
		var s serviceData
		json.Unmarshal(d, &s)

		for _, v := range monitorEnvList {
			if s.Name == v {
				for _, cluster := range s.Children {
					var serverList []string
					for _, node := range cluster.Data {
						serverList = append(serverList, fmt.Sprintf("http://%s:%s", node.Host, node.Port))
					}

					if len(serverList) > 0 {
						go retryFailedShard(serverList, false, cluster.User, cluster.Passwd)
					}
				}
			}
		}
	}
}

func runGetTaskLog() {
	for _, service := range Service().All().([]interface{}) {
		d, _ := json.Marshal(service.(map[string]interface{})["_source"])
		var s serviceData
		json.Unmarshal(d, &s)

		for _, v := range monitorEnvList {
			if s.Name == v {
				for _, cluster := range s.Children {
					var serverList []string
					for _, node := range cluster.Data {
						serverList = append(serverList, fmt.Sprintf("http://%s:%s", node.Host, node.Port))
					}

					if len(serverList) > 0 {
						go getTaskLog(cluster.Name, serverList, cluster.User, cluster.Passwd)
					}
				}
			}
		}
	}
}

func getServer(servers []string) (server string) {
	for _, s := range servers {
		u, _ := url.Parse(s)
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(u.Hostname(), u.Port()), time.Second*3)
		if err == nil {
			server = s
			conn.Close()
			break
		}
	}

	return
}

func initConfig() {
	if _, err := os.Stat(configName); os.IsNotExist(err) {
		defaultConfig := Config{
			IndexName:   "estasklog",
			TaskActions: "*search,*scroll",
			MonitorEnvs: []string{"prod"},
			WriteDataEs: []string{
				"http://192.168.10.1:9210",
				"http://192.168.10.2:9210",
				"http://192.168.10.3:9210",
				"http://192.168.10.4:9210",
			},
			WriteDataEsUser:   "elastic",
			WriteDataEsPasswd: "changeme",
		}

		file, err := os.Create(configName)
		if err != nil {
			log.Fatalf("Error creating config file: %v", err)
		}
		defer file.Close()

		encoder := toml.NewEncoder(file)
		err = encoder.Encode(defaultConfig)
		if err != nil {
			log.Fatalf("Error encoding config: %v", err)
		}
	}

	var conf Config
	if _, err := toml.DecodeFile("config.toml", &conf); err != nil {
		log.Fatal(err)
	}

	indexName = conf.IndexName
	taskActions = conf.TaskActions
	monitorEnvList = conf.MonitorEnvs
	writeDataEs = conf.WriteDataEs
	writeDataEsUser = conf.WriteDataEsUser
	writeDataEsPasswd = conf.WriteDataEsPasswd
}

func init() {
	if strings.Contains(os.Args[0], "go-build") {
		gin.SetMode(gin.DebugMode)
	} else {
		logFile, _ := os.OpenFile("estm.log", os.O_CREATE|os.O_APPEND|os.O_RDWR, 0644)
		logger = log.New(logFile, "[ESTM] ", log.Lshortfile|log.LstdFlags)
		gin.DefaultWriter = io.MultiWriter(logFile)
		gin.SetMode(gin.ReleaseMode)
	}

	initConfig()

	var err error
	es, err = elasticsearch.NewClient(elasticsearch.Config{
		Addresses: writeDataEs,
		Username:  writeDataEsUser,
		Password:  writeDataEsPasswd,
	})

	if err != nil {
		logger.Fatal(err)
	}

	c := cron.New()
	c.AddFunc("@every 500ms", runGetTaskLog)
	c.AddFunc("@every 1h", runRetryFailedShard)
	c.Start()
}

func main() {
	r := gin.Default()

	templ := template.Must(template.New("").ParseFS(FS, "templates/*.html"))
	r.SetHTMLTemplate(templ)

	f, _ := fs.Sub(FS, "templates/static")
	r.StaticFS("/static", http.FS(f))

	r.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "index.html", gin.H{})
	})

	r.GET("/getHistoryData", func(c *gin.Context) {
		query := c.QueryMap("query")
		c.JSON(http.StatusOK, getHistoryData(query))
	})

	r.GET("/service", func(c *gin.Context) {
		c.JSON(http.StatusOK, Service().All())
	})

	r.GET("/service/:id", func(c *gin.Context) {
		id := c.Param("id")
		c.JSON(http.StatusOK, Service().Get(id))
	})

	r.PUT("/service", func(c *gin.Context) {
		var d serviceData
		c.ShouldBindJSON(&d)

		err := Service().Create(d)
		if err != nil {
			logger.Println(err)
			c.JSON(http.StatusInternalServerError, gin.H{"message": "添加失败"})
		} else {
			c.JSON(http.StatusOK, gin.H{"message": "添加成功"})
		}
	})

	r.POST("/service/:id", func(c *gin.Context) {
		var d serviceData
		c.ShouldBindJSON(&d)

		id := c.Param("id")
		err := Service().Update(id, d)
		if err != nil {
			logger.Println(err)
			c.JSON(http.StatusInternalServerError, gin.H{"message": "添加失败"})
		} else {
			c.JSON(http.StatusOK, gin.H{"message": "添加成功"})
		}
	})

	r.DELETE("/service/:id", func(c *gin.Context) {
		id := c.Param("id")
		err := Service().Delete(id)
		if err != nil {
			logger.Println(err)
			c.JSON(http.StatusInternalServerError, gin.H{"message": "删除失败"})
		} else {
			c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
		}
	})

	r.POST("/checkEsService", func(c *gin.Context) {
		var d checkSrvData
		c.ShouldBindJSON(&d)

		c.JSON(http.StatusOK, gin.H{"status": checkEsService(d)})
	})

	r.POST("/retryShard", func(c *gin.Context) {
		var d []checkSrvData
		c.ShouldBindJSON(&d)

		for _, v := range d {
			if checkEsService(v) == 1 {
				retryFailedShard([]string{fmt.Sprintf("http://%s:%s", v.Host, v.Port)}, true)
				break
			}
		}

		c.JSON(http.StatusOK, gin.H{"status": 1})
	})

	r.POST("/action", func(c *gin.Context) {
		a := c.Query("a")

		var d struct {
			Host    string `json:"host"`
			Port    string `json:"port"`
			Systemd string `json:"systemd"`
		}
		c.ShouldBindJSON(&d)

		cmd := exec.Command("sh", "-c", fmt.Sprintf("eval `ssh-agent` ssh-add ~/.ssh/jump_id_rsa && ssh -o StrictHostKeyChecking=no root@%s systemctl %s %s", d.Host, a, d.Systemd))
		out, err := cmd.CombinedOutput()
		if err != nil {
			logger.Println(err)
			c.JSON(http.StatusInternalServerError, gin.H{"message": string(out)})
		} else {
			c.JSON(http.StatusOK, gin.H{})
		}

	})

	r.POST("/getRealData", func(c *gin.Context) {
		var d esUrlData
		c.ShouldBindJSON(&d)

		c.JSON(http.StatusOK, getRealData(d))
	})

	r.POST("/cancelTask", func(c *gin.Context) {
		var d struct {
			esUrlData
			tid string
		}
		c.ShouldBindJSON(&d)

		if cancelTask(d.tid, d.Servers, d.User, d.Passwd) {
			c.JSON(http.StatusOK, gin.H{})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{})
		}
	})

	r.GET("/status", func(c *gin.Context) {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		c.JSON(http.StatusOK, gin.H{"threading": runtime.NumGoroutine(), "memory": m.Alloc})
	})

	r.Run(":8088")
}
