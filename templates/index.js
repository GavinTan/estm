const serviceView = {
    template: '#service',
    delimiters: ["{%", "%}"],
    data() {
        return {
            search: '',
            tableData: {
                cid: '',
                data: [],
            },
            sform: {
                host: '',
                systemd: '',
                port: '9200',
                remark: ''
            },
            sformRules: {
                host: [
                    { required: true, trigger: 'blur' },
                ],
                systemd: [
                    { required: true, trigger: 'blur' },
                ],
                port: [
                    { required: true, trigger: 'blur' },
                ],
            },
            gform: {
                name: '',
                kibana: ''
            },
            gformRules: {
                name: [
                    { required: true, message: '', trigger: 'blur' },
                ],
            },
            treeData: [{
                tid: '0',
                name: 'ES集群',
                type: 'base',
                number: '',
                children: []
            }],
            srvData: [],
            expands: ['0'],
            defaultProps: {
                children: 'children',
                label: 'name'
            },
            filterText: '',
            leftClickTreeNode: {},
            contextMenuVisible: false,
            clickNode: {},
            addGroupVisible: false,
            addEsVisible: false,
            addServiceVisible: false,
            editService: false,
            editNode: false,
        }
    },
    computed: {
        tables() {
            const search = this.search;
            if (search) {
                return this.tableData.data.filter(item => {
                    return Object.keys(item).some(key => {
                        return String(item[key]).toLowerCase().indexOf(search) > -1
                    });
                });
            }
            return this.tableData.data;
        }
    },
    watch: {
        filterText(val) {
            this.$refs.tree.filter(val);
        }
    },
    created() {
        this.fetchTreeData();

        // this.timer = setInterval(() => {
        //     this.checkEsService()
        // }, 1000 * 30)
    },
    methods: {
        fetchTreeData() {
            axios.get("/service").then(res => {
                const data = [];
                res.data?.forEach((g, gindex) => {
                    data.push({
                        name: g?._source.name,
                        id: g._id,
                        tid: `0-${gindex}`,
                        type: 'group',
                        number: g?._source?.children?.length,
                        children: g?._source?.children?.map((c, sindex) => ({
                            name: c.name,
                            id: g._id,
                            cid: c.cid,
                            tid: `0-${gindex}-${sindex}`,
                            type: 'cluster',
                            kibana: c.kibana
                        })) || []
                    });
                });
                this.treeData[0].children = data;
            });
        },
        fetchData(id, cid) {
            axios.get(`/service/${id}`).then(res => {
                res.data?.children.forEach(i => {
                    if (i.cid === cid) {
                        this.$set(this.tableData, 'data', i?.data?.map((ii, index) => Object.assign(ii, { id: index + 1 })) || []);
                        this.$set(this.tableData, 'cid', i.cid);
                    }
                });

                this.srvData = res.data?.children || [];

                this.$nextTick(() => {
                    this.checkEsService();
                });
            })

        },
        async checkEsService() {
            const cid = this.tableData.cid
            for (let i = 0; i < this.tableData.data.length; i++) {
                const item = this.tableData.data[i];
                axios.post("/checkEsService", { host: item.host, port: item.port }).then(res => {
                    if (this.clickNode.cid === cid) {
                        this.$set(this.tableData.data, i, { ...item, status: res.data.status });
                    };
                }).catch(() => {
                    this.$set(this.tableData.data, i, { ...item, status: -1 });
                });

            };
        },
        filterNode(value, data) {
            if (!value) return true;
            return data.name.indexOf(value) !== -1;
        },
        hidePanel(e) {
            this.contextMenuVisible = false;
            document.removeEventListener('click', this.hidePanel, false);
            this.$refs.menu.style.display = 'none';
        },
        handleRightClick(MouseEvent, object, Node, element) {
            document.addEventListener('click', this.hidePanel, false);
            this.contextMenuVisible = true;
            this.clickNode = Node.data;
            const menu = this.$refs.menu;
            menu.style.display = 'block';
            menu.style.left = MouseEvent.clientX - 0 + 'px';
            menu.style.top = MouseEvent.clientY - 0 + 'px';
        },
        handleNodeClick(data) {
            this.expands = ['0'];
            this.contextMenuVisible = false;
            this.$refs.menu.style.display = 'none';
            this.clickNode = data;

            if (data.type === 'cluster') {
                this.fetchData(data.id, data.cid);
            };
        },
        hendleRename() {
            this.gform.name = this.clickNode.name;
            if (this.clickNode.type === 'cluster') {
                this.gform.kibana = this.clickNode.kibana;
            };
            this.addGroupVisible = true;
            this.editNode = true;
            axios.get(`/service/${this.clickNode.id}`).then(res => {
                this.srvData = res?.data || [];
            })
        },
        handleAddGroup() {
            this.$refs.gform.validate(valid => {
                if (valid) {
                    const data = { name: this.gform.name };
                    axios.put("/service", { data }).then(res => {
                        this.fetchTreeData();
                        this.$message.success(res.data?.message || res.statusText);
                        this.addGroupVisible = false;
                        this.$refs.gform.resetFields();
                    }).catch(error => {
                        this.$message.error(error.response.data?.message || error);
                    });
                } else {
                    return false;
                }
            });
        },
        handleDelGroup() {
            axios.delete(`/service/${this.clickNode.id}`).then(res => {
                this.fetchTreeData();
                this.tableData = [];
                this.$message.success(res.data?.message || res.statusText);
            }).catch(error => {
                this.$message.error(error.response.data?.message || error);
            });
        },
        handleAddCluster() {
            this.$refs.gform.validate(async valid => {
                if (valid) {
                    let data = { children: [] };

                    try {
                        const res = await axios.get(`/service/${this.clickNode.id}`)

                        if (this.editNode) {
                            if (this.clickNode.type === 'group') {
                                data = this.srvData;
                                data.name = this.gform.name;
                                delete data.updatedAt;
                            }

                            if (this.clickNode.type === 'cluster') {
                                data.children = res.data.children.map(i => {
                                    if (i.cid === this.clickNode.cid) {
                                        i.name = this.gform.name;
                                        i.kibana = this.gform.kibana;
                                    }
                                    return i;
                                });
                            }
                        } else {

                            if (res.data.children) {
                                data.children = [...res.data.children, { name: this.gform.name, cid: new Date().getTime(), kibana: this.gform.kibana }];
                            } else {
                                data.children.push({ name: this.gform.name, cid: new Date().getTime(), kibana: this.gform.kibana });
                            }
                        }
                    } catch (error) {
                        this.$message.error(error);
                    };

                    axios.post(`/service/${this.clickNode.id}`, data).then(res => {
                        this.fetchTreeData();
                        this.expands.push(this.clickNode.tid);
                        this.$message.success(res.data?.message || res.statusText);
                        this.addGroupVisible = false;
                        this.$refs.gform.resetFields();
                        this.editNode = false;
                    }).catch(error => {
                        this.$message.error(error.response.data?.message || error);
                    });
                } else {
                    return false;
                }
            });
        },
        handleDelCluster() {
            axios.get(`/service/${this.clickNode.id}`).then(res => {
                const data = {
                    children: res.data?.children.filter(i => i.cid !== this.clickNode.cid)
                };

                axios.post(`/service/${this.clickNode.id}`, data).then(res => {
                    this.fetchTreeData();
                    this.tableData = [];
                    this.$message.success('删除成功');
                }).catch(error => {
                    this.$message.error('删除失败');
                });
            })
        },
        handleAddService() {
            this.$refs.sform.validate(async valid => {
                if (valid) {
                    try {
                        const res = await axios.get(`/service/${this.clickNode.id}`);
                        const children = res.data.children.map(i => {
                            if (i.cid === this.clickNode.cid) {
                                const data = i.data || [];
                                if (this.editService) {
                                    data.forEach((s, sindex) => {
                                        if (s.sid === this.sform.sid) {
                                            data[sindex] = this.sform;
                                        }
                                    });
                                } else {
                                    data.push(Object.assign(this.sform, { sid: new Date().getTime() }));
                                }
                                i.data = data;
                            }
                            return i;
                        })
                        const data = { children };

                        axios.post(`/service/${this.clickNode.id}`, data).then(res => {
                            this.addServiceVisible = false;
                            this.fetchData(this.clickNode.id, this.clickNode.cid);
                            if (this.editService) {
                                this.$message.success('更新成功');
                            } else {
                                this.$message.success(res.data?.message || res.statusText);
                            }
                            this.editService = false;
                        }).catch(error => {
                            this.$message.error(error.response.data?.message || error);
                        });
                    } catch (error) {
                        this.$message.error(error);
                    };
                } else {
                    return false;
                }
            });
        },
        handleDelService(sid) {
            const children = this.srvData.map(i => {
                if (i.name === this.clickNode.name) {
                    const data = i.data.filter(i => i.sid !== sid);
                    return { name: i.name, data };
                };
                return i;
            })
            const data = { children };

            axios.post(`/service/${this.clickNode.id}`, data).then(res => {
                this.fetchData(this.clickNode.id, this.clickNode.cid);
                this.$message.success('删除成功');
            }).catch(error => {
                this.$message.error('删除失败');
            });
        },
        handleEditService(data) {
            this.sform = data;
            this.addServiceVisible = true;
            this.editService = true;
        },
        handleService(a, data) {
            axios.post(`/action?a=${a}`, data).then(res => {
                this.checkEsService();
            }).catch(error => {
                this.$message.error(error.response.data?.message || error);
            });
        },
    },
    beforeDestroy() {
        clearInterval(this.timer);
    }
}

const realView = {
    template: '#real',
    delimiters: ["{%", "%}"],
    data() {
        return {
            search: '',
            dsl: '',
            dslVisible: false,
            tableData: [],
            searchCluster: 'es-cluster-prod',
            clusterOptions: [],
            esClusterData: {},
        }
    },
    computed: {
        tables() {
            {
                const search = this.search;
                if (search) {
                    return this.tableData.filter(item => {
                        return Object.keys(item).some(key => {
                            return String(item[key]).toLowerCase().indexOf(search) > -1;
                        });
                    })
                }
                return this.tableData;
            }
        }
    },
    created() {
        axios.get("/service").then(res => {
            res.data?.forEach(service => {
                service._source?.children?.forEach(c => {
                    this.clusterOptions.push({ label: c.name, value: c.name });
                    this.esClusterData[c.name] = c.data.map(i => `http://${i.host}:${i.port}`);
                });
            });

            this.fetchData();
        });

        //this.timer = setInterval(() => {
        //  setTimeout(this.fetchData(), 0)
        //}, 1000 * 30)
    },
    methods: {
        fetchData() {
            axios.get(`/getRealData?s=${this.esClusterData[this.searchCluster]}`).then(({ data }) => {
                this.tableData = data;
            });
        },
        showDialog(d) {
            this.dsl = d;
            this.dslVisible = true;
        },
        cancelTask(row) {
            axios.post("/cancelTask", { tid: row.id }, { headers: { 'Content-Type': 'multipart/form-data' } }).then(() => {
                this.$message.success('取消任务成功！');
            }).catch(() => {
                this.$message.error('取消任务失败！');
            });
        }
    },
    //beforeDestroy() {
    //  clearInterval(this.timer);
    //}
}


const historyView = {
    template: '#history',
    delimiters: ["{%", "%}"],
    data() {
        return {
            search: '',
            searchNode: '',
            searchCluster: '',
            clusterOptions: [],
            qd: moment().format('YYYY-MM-DD'),
            qt: [moment(moment().startOf('day')).valueOf(), moment(moment().endOf('day')).valueOf()],
            dsl: '',
            dslVisible: false,
            tableData: [],
            pickerOptions: {
                disabledDate(time) {
                    return time.getTime() > Date.now() || Math.ceil((Date.now() - time.getTime()) / (1000 * 3600 * 24)) > 30;
                },
            },
            tableTotal: 0,
            tableCurrentPage: 1,
            tablePageSize: 10,
            order: '',
            orderField: '',
        }
    },
    created() {
        this.fetchData();

        axios.get("/service").then(res => {
            res.data?.forEach(service => {
                service._source?.children?.forEach(cluster => this.clusterOptions.push({ label: cluster.name, value: cluster.name }));
            });
        });
    },
    methods: {
        moment,
        fetchData() {
            let qt;
            if (this.qt === null) {
                qt = [];
            } else {
                qt = [
                    moment(this.qt[0]).add(moment(this.qd).diff(moment(moment(this.qt[0]).format('YYYY-MM-DD')), 'day'), 'day').valueOf(),
                    moment(this.qt[1]).add(moment(this.qd).diff(moment(moment(this.qt[1]).format('YYYY-MM-DD')), 'day'), 'day').valueOf()
                ];
            };

            let from = 0;
            if (this.tableCurrentPage !== 1) {
                from = (this.tableCurrentPage - 1) * this.tablePageSize;
            };

            const params = {
                "query[qd]": this.qd,
                "query[qt]": `${qt}`,
                "query[qn]": this.searchNode,
                "query[q]": this.search,
                "query[qc]": this.searchCluster,
                "query[from]": from,
                "query[size]": this.tablePageSize,
                "query[sortOrder]": this.order,
                "query[sortField]": this.orderField
            };

            axios({
                url: 'getHistoryData',
                method: 'get',
                params
            }).then(({ data }) => {
                this.tableData = data.data;
                this.tableTotal = data.total;
                // document.querySelector('.main-container').scrollIntoView();
            });
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
            this.order = col.order?.replace('ending', '') ? col.order?.replace('ending', '') : '';
            this.orderField = col.prop?.replace('_source.', '');
            this.fetchData();
        },
        handleQuerydate() {
            this.tableCurrentPage = 1;
            this.fetchData();
        },
    }
}

const routes = [
    { path: '/estask/real', component: realView },
    { path: '/estask/history', component: historyView },
    { path: '/service', component: serviceView },
    { path: '*', redirect: '/service' }
];

const router = new VueRouter({
    routes
});


const app = new Vue({
    router,
    delimiters: ["{%", "%}"],
    data: {
        isCollapse: false
    }
}).$mount('#app');