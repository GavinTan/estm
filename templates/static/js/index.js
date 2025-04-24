const { reactive, ref } = Vue;
const { ElMessage } = ElementPlus;

const serviceView = {
  template: "#service",
  delimiters: ["{%", "%}"],
  data() {
    const setupVisible = ref(false);
    const tableData = reactive({
      cid: "",
      data: [],
    });
    const treeData = reactive([
      {
        tid: "0",
        name: "ES集群",
        type: "base",
        number: "",
        children: [],
      },
    ]);

    return {
      search: "",
      tableData,
      sform: {
        host: "",
        systemd: "",
        port: "9200",
        remark: "",
      },
      sformRules: {
        host: [{ required: true, trigger: "blur" }],
        systemd: [{ required: true, trigger: "blur" }],
        port: [{ required: true, trigger: "blur" }],
      },
      gform: {
        name: "",
        kibana: "",
        user: "",
        passwd: "",
      },
      gformRules: {
        name: [{ required: true, message: "", trigger: "blur" }],
      },
      treeData,
      srvData: [],
      expands: ["0"],
      defaultProps: {
        children: "children",
        label: "name",
      },
      esClusterData: {},
      selectEsCluster: "",
      filterText: "",
      leftClickTreeNode: {},
      contextMenuVisible: false,
      clickNode: {},
      addGroupVisible: false,
      addEsVisible: false,
      addServiceVisible: false,
      editService: false,
      editNode: false,
      loading: false,
      setupVisible,
    };
  },
  computed: {
    tables() {
      const search = this.search;
      if (search) {
        return this.tableData.data.filter((item) => {
          return Object.keys(item).some((key) => {
            return String(item[key]).toLowerCase().indexOf(search) > -1;
          });
        });
      }
      return this.tableData.data;
    },
  },
  watch: {
    filterText(val) {
      this.$refs.tree.filter(val);
    },
  },
  created() {
    this.fetchTreeData();

    this.timer = setInterval(() => {
      this.checkEsService();
    }, 1000 * 30);
  },
  methods: {
    fetchTreeData() {
      this.loading = true;
      axios
        .get("/service")
        .then((res) => {
          const data = [];
          res.data?.forEach((g, gindex) => {
            data.push({
              name: g?._source.name,
              id: g._id,
              tid: `0-${gindex}`,
              type: "group",
              number: g?._source?.children?.length,
              children:
                g?._source?.children?.map((c, sindex) => {
                  this.esClusterData[c.name] = c.data;

                  return {
                    name: c.name,
                    id: g._id,
                    cid: c.cid,
                    tid: `0-${gindex}-${sindex}`,
                    type: "cluster",
                    kibana: c.kibana,
                    user: c.user,
                    passwd: c.passwd,
                  };
                }) || [],
            });
          });
          this.treeData[0].children = data;
        })
        .catch(() => this.$message.error("请求出错！"))
        .finally(() => (this.loading = false));
    },
    fetchData(id, cid) {
      axios
        .get(`/service/${id}`)
        .then((res) => {
          res.data?.children.forEach((i) => {
            if (i.cid === cid) {
              this.tableData.data =
                i?.data?.map((ii, index) =>
                  Object.assign(ii, { id: index + 1 })
                ) || [];
              this.tableData.cid = i.cid;
            }
          });

          this.srvData = res.data?.children || [];

          this.$nextTick(() => {
            this.checkEsService();
          });
        })
        .catch((err) => {
          this.$message.error("请求出错！");
        });
    },
    async checkEsService() {
      const cid = this.tableData.cid;
      for (let i = 0; i < this.tableData.data.length; i++) {
        const item = this.tableData.data[i];
        axios
          .post("/checkEsService", { host: item.host, port: item.port })
          .then((res) => {
            if (this.clickNode.cid === cid) {
              this.tableData.data[i] = {
                ...item,
                status: res.data.status,
              };
            }
          })
          .catch(() => {
            this.tableData.data[i] = { ...item, status: -1 };
          });
      }
    },
    filterNode(value, data) {
      if (!value) return true;
      return data.name.indexOf(value) !== -1;
    },
    hidePanel(e) {
      this.contextMenuVisible = false;
      document.removeEventListener("click", this.hidePanel, false);
      this.$refs.menu.style.display = "none";
    },
    handleRightClick(MouseEvent, object, Node, element) {
      document.addEventListener("click", this.hidePanel, false);
      this.contextMenuVisible = true;
      this.clickNode = Node.data;
      const menu = this.$refs.menu;
      menu.style.display = "block";
      menu.style.left = MouseEvent.clientX - 0 + "px";
      if (window.innerHeight - MouseEvent.clientY < 142) {
        menu.style.top = MouseEvent.clientY - 142 + "px";
      } else {
        menu.style.top = MouseEvent.clientY - 0 + "px";
      }
    },
    handleNodeClick(data) {
      this.expands = ["0"];
      this.contextMenuVisible = false;
      this.$refs.menu.style.display = "none";
      this.clickNode = data;

      if (data.type === "cluster") {
        this.fetchData(data.id, data.cid);
      }
    },
    hendleRename() {
      this.gform.name = this.clickNode.name;
      if (this.clickNode.type === "cluster") {
        this.gform.kibana = this.clickNode.kibana;
        this.gform.user = this.clickNode.user;
        this.gform.passwd = this.clickNode.passwd;
      }
      this.addGroupVisible = true;
      this.editNode = true;
      axios.get(`/service/${this.clickNode.id}`).then((res) => {
        this.srvData = res?.data || [];
      });
    },
    handleAddGroup() {
      this.$refs.gform.validate((valid) => {
        if (valid) {
          axios
            .put("/service", { name: this.gform.name })
            .then((res) => {
              this.fetchTreeData();
              this.$message.success(res.data?.message || res.statusText);
              this.addGroupVisible = false;
              this.$refs.gform.resetFields();
            })
            .catch((error) => {
              this.$message.error(error.response.data?.message || error);
            });
        } else {
          return false;
        }
      });
    },
    handleDelGroup() {
      axios
        .delete(`/service/${this.clickNode.id}`)
        .then((res) => {
          this.fetchTreeData();
          this.tableData = [];
          this.$message.success(res.data?.message || res.statusText);
        })
        .catch((error) => {
          this.$message.error(error.response.data?.message || error);
        });
    },
    handleAddCluster() {
      this.$refs.gform.validate(async (valid) => {
        if (valid) {
          let data = { children: [] };

          try {
            const res = await axios.get(`/service/${this.clickNode.id}`);

            if (this.editNode) {
              if (this.clickNode.type === "group") {
                data = this.srvData;
                data.name = this.gform.name;
                delete data.updatedAt;
              }

              if (this.clickNode.type === "cluster") {
                data.children = res.data.children.map((i) => {
                  if (i.cid === this.clickNode.cid) {
                    i.name = this.gform.name;
                    i.kibana = this.gform.kibana;
                    i.user = this.gform.user;
                    i.passwd = this.gform.passwd;
                  }
                  return i;
                });
              }
            } else {
              if (res.data.children) {
                data.children = [
                  ...res.data.children,
                  {
                    name: this.gform.name,
                    cid: new Date().getTime(),
                    kibana: this.gform.kibana,
                    user: this.gform.user,
                    passwd: this.gform.passwd,
                  },
                ];
              } else {
                data.children.push({
                  name: this.gform.name,
                  cid: new Date().getTime(),
                  kibana: this.gform.kibana,
                  user: this.gform.user,
                  passwd: this.gform.passwd,
                });
              }
            }
          } catch (error) {
            this.$message.error(error);
          }

          axios
            .post(`/service/${this.clickNode.id}`, data)
            .then((res) => {
              this.fetchTreeData();
              if (this.expands.indexOf(this.clickNode.tid) === -1) {
                this.expands.push(this.clickNode.tid);
              }
              this.$message.success(res.data?.message || res.statusText);
              this.addGroupVisible = false;
              // this.$refs.gform.resetFields();
              this.editNode = false;
            })
            .catch((error) => {
              this.$message.error(error.response.data?.message || error);
            });
        } else {
          return false;
        }
      });
    },
    handleDelCluster() {
      axios.get(`/service/${this.clickNode.id}`).then((res) => {
        const data = {
          children: res.data?.children.filter(
            (i) => i.cid !== this.clickNode.cid
          ),
        };

        axios
          .post(`/service/${this.clickNode.id}`, data)
          .then((res) => {
            this.fetchTreeData();
            this.tableData = [];
            this.$message.success("删除成功");
          })
          .catch((error) => {
            this.$message.error("删除失败");
          });
      });
    },
    handleAddService() {
      this.$refs.sform.validate(async (valid) => {
        if (valid) {
          try {
            const res = await axios.get(`/service/${this.clickNode.id}`);
            const children = res.data.children.map((i) => {
              if (i.cid === this.clickNode.cid) {
                const data = i.data || [];
                if (this.editService) {
                  data.forEach((s, sindex) => {
                    if (s.sid === this.sform.sid) {
                      data[sindex] = this.sform;
                    }
                  });
                } else {
                  data.push(
                    Object.assign(this.sform, { sid: new Date().getTime() })
                  );
                }
                i.data = data;
              }
              return i;
            });
            const data = { children };

            axios
              .post(`/service/${this.clickNode.id}`, data)
              .then((res) => {
                this.addServiceVisible = false;
                this.fetchData(this.clickNode.id, this.clickNode.cid);
                if (this.editService) {
                  this.$message.success("更新成功");
                } else {
                  this.$message.success(res.data?.message || res.statusText);
                }
                this.editService = false;
              })
              .catch((error) => {
                this.$message.error(error.response.data?.message || error);
              });
          } catch (error) {
            this.$message.error(error);
          }
        } else {
          return false;
        }
      });
    },
    handleDelService(sid) {
      const children = this.srvData.map((i) => {
        if (i.name === this.clickNode.name) {
          const data = i.data.filter((i) => i.sid !== sid);
          return { name: i.name, data };
        }
        return i;
      });
      const data = { children };

      axios
        .post(`/service/${this.clickNode.id}`, data)
        .then((res) => {
          this.fetchData(this.clickNode.id, this.clickNode.cid);
          this.$message.success("删除成功");
        })
        .catch((error) => {
          this.$message.error("删除失败");
        });
    },
    handleEditService(data) {
      this.sform = data;
      this.addServiceVisible = true;
      this.editService = true;
    },
    handleService(a, data) {
      axios
        .post(`/action?a=${a}`, data)
        .then((res) => {
          this.$message.success("操作成功");

          setTimeout(() => {
            this.checkEsService();
          }, 2000);
        })
        .catch((error) => {
          this.$message.error(error.response.data?.message || error);
        });
    },
    handleRetryShard() {
      axios
        .post("/retryShard", this.esClusterData[this.selectEsCluster] || [])
        .then(() =>
          ElMessage({
            showClose: true,
            message: "执行成功",
            type: "success",
          })
        )
        .catch(
          err >
            ElMessage({
              duration: 5000,
              showClose: true,
              message: err,
              type: "error",
            })
        );
    },
  },
  beforeDestroy() {
    clearInterval(this.timer);
  },
};

const realView = {
  template: "#real",
  delimiters: ["{%", "%}"],
  data() {
    return {
      search: "",
      dsl: "",
      dslVisible: false,
      tableData: [],
      searchCluster: "es-cluster-prod",
      clusterOptions: [],
      esClusterData: {},
      loading: false,
      clickDownTime: "",
      autoRefreshTimer: null,
    };
  },
  computed: {
    tables() {
      {
        const search = this.search;
        if (search) {
          return this.tableData.filter((item) => {
            return Object.keys(item).some((key) => {
              return String(item[key]).toLowerCase().indexOf(search) > -1;
            });
          });
        }
        return this.tableData;
      }
    },
  },
  created() {
    axios.get("/service").then((res) => {
      res.data?.forEach((service) => {
        service._source?.children?.forEach((c) => {
          this.clusterOptions.push({ label: c.name, value: c.name });
          this.esClusterData[c.name] = {
            servers: c.data.map((i) => `http://${i.host}:${i.port}`),
            user: c.user,
            passwd: c.passwd,
          };
        });
      });

      this.fetchData();
    });
  },
  methods: {
    fetchData() {
      this.loading = true;
      axios
        .post("/getRealData", this.esClusterData[this.searchCluster])
        .then(({ data }) => {
          this.tableData = data;
        })
        .catch(() => this.$message.error("请求出错！"))
        .finally(() => (this.loading = false));
    },
    showDialog(d) {
      this.dsl = d;
      this.dslVisible = true;
    },
    cancelTask(row) {
      axios
        .post(`/cancelTask`, {
          ...this.esClusterData[this.searchCluster],
          tid: row.id,
        })
        .then(() => this.$message.success("取消任务成功！"))
        .catch(() => this.$message.error("取消任务失败！"));
    },
    refreshOnDown() {
      this.clickDownTime = new Date();

      this.longPressTimeout = setTimeout(() => {
        this.$message.success("已开启自动刷新（10s）！");
        this.autoRefreshTimer = setInterval(() => {
          setTimeout(this.fetchData(), 0);
        }, 1000 * 10);

        clearTimeout(this.longPressTimeout);
      }, 2000);

      this.fetchData();
    },
    refreshOnUp() {
      if (new Date() - this.clickDownTime < 2000) {
        clearTimeout(this.longPressTimeout);
      }

      document.activeElement.blur();
    },
  },
  beforeDestroy() {
    clearInterval(this.autoRefreshTimer);
  },
};

const historyView = {
  template: "#history",
  delimiters: ["{%", "%}"],
  data() {
    return {
      search: "",
      searchNode: "",
      searchCluster: "",
      clusterOptions: [],
      qd: dayjs().format("YYYY-MM-DD"),
      qt: [dayjs().startOf("day").valueOf(), dayjs().endOf("day").valueOf()],
      dsl: "",
      dslVisible: false,
      tableData: [],
      pickerOptions: {
        disabledDate(time) {
          return (
            time.getTime() > Date.now() ||
            Math.ceil((Date.now() - time.getTime()) / (1000 * 3600 * 24)) > 30
          );
        },
      },
      tableTotal: 0,
      tableCurrentPage: 1,
      tablePageSize: 10,
      order: "",
      orderField: "",
      loading: false,
    };
  },
  created() {
    this.fetchData();

    axios.get("/service").then((res) => {
      res.data?.forEach((service) => {
        service._source?.children?.forEach((cluster) =>
          this.clusterOptions.push({ label: cluster.name, value: cluster.name })
        );
      });
    });
  },
  methods: {
    fetchData() {
      let qt;
      if (this.qt === null) {
        qt = [];
      } else {
        qt = [
          dayjs(this.qt[0])
            .add(
              dayjs(this.qd).diff(
                dayjs(dayjs(this.qt[0]).format("YYYY-MM-DD")),
                "day"
              ),
              "day"
            )
            .valueOf(),
          dayjs(this.qt[1])
            .add(
              dayjs(this.qd).diff(
                dayjs(dayjs(this.qt[1]).format("YYYY-MM-DD")),
                "day"
              ),
              "day"
            )
            .valueOf(),
        ];
      }

      let from = 0;
      if (this.tableCurrentPage !== 1) {
        from = (this.tableCurrentPage - 1) * this.tablePageSize;
      }

      const params = {
        "query[qd]": this.qd,
        "query[qt]": `${qt}`,
        "query[qn]": this.searchNode,
        "query[q]": this.search,
        "query[qc]": this.searchCluster,
        "query[from]": from,
        "query[size]": this.tablePageSize,
        "query[sortOrder]": this.order,
        "query[sortField]": this.orderField,
      };

      this.loading = true;

      axios({
        url: "getHistoryData",
        method: "get",
        params,
      })
        .then(({ data }) => {
          this.tableData = data.data;
          this.tableTotal = data.total;
          // document.querySelector('.main-container').scrollIntoView();
        })
        .catch(() => this.$message.error("请求出错！"))
        .finally(() => (this.loading = false));
    },
    showDialog(d) {
      this.dsl = d.match(/source\[(.*?)\]$/)[1];
      this.dslVisible = true;
    },
    handleFilterData() {
      this.tableCurrentPage = 1;
      this.fetchData();
    },
    handleSizeChange(val) {
      this.tablePageSize = val;
      this.fetchData();
    },
    handleCurrentChange(val) {
      this.tableCurrentPage = val;
      this.fetchData();
    },
    handleSort(col) {
      this.order = col.order?.replace("ending", "")
        ? col.order?.replace("ending", "")
        : "";
      this.orderField = col.prop?.replace("_source.", "");
      this.fetchData();
    },
    handleQuerydate() {
      this.tableCurrentPage = 1;
      this.fetchData();
    },
    handleRefresh() {
      this.fetchData();
      document.activeElement.blur();
    },
  },
};

const routes = [
  { path: "/estask/real", component: realView },
  { path: "/estask/history", component: historyView },
  { path: "/service", component: serviceView },
  { path: "/:pathMatch(.*)*", redirect: "/service" },
];

const router = VueRouter.createRouter({
  history: VueRouter.createWebHashHistory(),
  routes,
});

const App = {
  delimiters: ["{%", "%}"],
  data() {
    const isCollapse = ref(window.screen.width < 768);
    window.addEventListener("resize", () => {
      isCollapse.value = window.screen.width < 768;
    });

    return {
      isCollapse,
    };
  },
};

const app = Vue.createApp(App);

for (const [key, component] of Object.entries(ElementPlusIconsVue)) {
  app.component(key, component);
}

app.config.globalProperties.dayjs = dayjs;

app.use(ElementPlus, {
  locale: ElementPlusLocaleZhCn,
});
app.use(router);
app.mount("#app");
