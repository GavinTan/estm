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
	"strings"
	"sync/atomic"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/elastic/go-elasticsearch/v7"
	"github.com/elastic/go-elasticsearch/v7/esutil"
	"github.com/gin-gonic/gin"
	"github.com/robfig/cron/v3"
)

//go:embed templates
var FS embed.FS

var (
	configName = "config.toml"

	client = &http.Client{
		Timeout: 10 * time.Second,
	}

	indexName      string
	taskActions    string
	monitorEnvList []string
	writeDataEs    []string

	lb *LoadBalancer
)

type Config struct {
	IndexName   string   `toml:"index_name"`
	TaskActions string   `toml:"task_actions"`
	MonitorEnvs []string `toml:"monitor_envs"`
	WriteDataEs []string `toml:"write_data_es"`
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
	} `json:"children"`
	CreatedAt int64 `json:"createdAt,omitempty"`
	UpdatedAt int64 `json:"updatedAt,omitempty"`
}

type servceObj struct {
	es        *elasticsearch.Client
	indexName string
}

type LoadBalancer struct {
	servers     []string
	index       int32
	serverCount int32
}

func NewLoadBalancer(servers []string) *LoadBalancer {
	return &LoadBalancer{
		servers:     servers,
		index:       0,
		serverCount: int32(len(servers)),
	}
}

func (lb *LoadBalancer) nextServer() string {
	index := atomic.AddInt32(&lb.index, 1) % lb.serverCount
	return lb.servers[index]
}

func getHistoryData(args map[string]string) interface{} {
	es, _ := elasticsearch.NewClient(elasticsearch.Config{
		Addresses: writeDataEs,
	})

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

func getRealData(services []string) interface{} {
	lb := NewLoadBalancer(services)
	resp, err := client.Get(fmt.Sprintf("%s/_tasks?actions=*search&detailed", lb.nextServer()))
	if err != nil {
		fmt.Println(err)
		return []retTaskData{}
	}

	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)

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

func cancelTask(tid string) bool {
	resp, err := client.PostForm(fmt.Sprintf("%s/_tasks/%s/_cancel", lb.nextServer(), tid), url.Values{})
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
	es, _ := elasticsearch.NewClient(elasticsearch.Config{
		Addresses: writeDataEs,
	})

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

	es, err := elasticsearch.NewClient(elasticsearch.Config{
		Addresses: writeDataEs,
	})

	if err != nil {
		log.Println(err)
		return
	}

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
					log.Println(err)
					return
				}

				defer res.Body.Close()
				io.Copy(io.Discard, res.Body)

				if res.IsError() {
					log.Println("write index failed!")
				}
			}

		}
	}
}

func getTaskLog(cluster, server string) {
	resp, err := client.Get(fmt.Sprintf("%s/_tasks?actions=%s&detailed", server, taskActions))
	if err != nil {
		log.Println(err)
		return
	}

	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var data map[string]map[string]nodeData
	json.Unmarshal(body, &data)

	writeTaskData(cluster, data)
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

					lb := NewLoadBalancer(serverList)
					go getTaskLog(cluster.Name, lb.nextServer())
				}
			}
		}
	}
}

func initConfig() {
	if _, err := os.Stat(configName); os.IsNotExist(err) {
		defaultConfig := Config{
			IndexName:   "estasklog",
			TaskActions: "*search,*scroll",
			MonitorEnvs: []string{"prod"},
			WriteDataEs: []string{
				"http://10.100.1.71:9210",
				"http://10.100.1.72:9210",
				"http://10.100.1.73:9210",
				"http://10.100.1.74:9210",
			},
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
}

func init() {
	log.SetPrefix("[ESTM] ")
	log.SetFlags(log.Lshortfile | log.LstdFlags)

	initConfig()

	c := cron.New()
	c.AddFunc("@every 500ms", runGetTaskLog)
	c.Start()
}

func main() {
	lb = NewLoadBalancer(writeDataEs)
	r := gin.Default()

	templ := template.Must(template.New("").ParseFS(FS, "templates/*.html"))
	r.SetHTMLTemplate(templ)

	f, _ := fs.Sub(FS, "templates")
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
			log.Println(err)
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
			log.Println(err)
			c.JSON(http.StatusInternalServerError, gin.H{"message": "添加失败"})
		} else {
			c.JSON(http.StatusOK, gin.H{"message": "添加成功"})
		}
	})

	r.DELETE("/service/:id", func(c *gin.Context) {
		id := c.Param("id")
		err := Service().Delete(id)
		if err != nil {
			log.Println(err)
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

	r.POST("/action", func(c *gin.Context) {
		a := c.Query("a")

		var d struct {
			Host    string `json:"host"`
			Port    string `json:"port"`
			Systemd string `json:"systemd"`
		}
		c.ShouldBindJSON(&d)

		cmd := exec.Command("ssh", fmt.Sprintf("root@%s", d.Host), fmt.Sprintf("systemctl %s %s", a, d.Systemd))
		out, err := cmd.CombinedOutput()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": string(out)})
		} else {
			c.JSON(http.StatusOK, gin.H{})
		}

	})

	r.GET("/getRealData", func(c *gin.Context) {
		var servers []string
		s := c.Query("s")

		if s != "" {
			servers = strings.Split(s, ",")
		}

		c.JSON(http.StatusOK, getRealData(servers))
	})

	r.POST("/cancelTask", func(c *gin.Context) {
		tid := c.PostForm("tid")

		if cancelTask(tid) {
			c.JSON(http.StatusOK, gin.H{})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{})
		}
	})

	r.Run(":8088")
}
