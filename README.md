# estm

es服务管理任务监控，通过es _tasks接口抓取当前的任务。

![image-20240410145641446](https://raw.githubusercontent.com/GavinTan/files/master/picgo/image-20240410145641446.png)



![image-20240410145453692](https://raw.githubusercontent.com/GavinTan/files/master/picgo/image-20240410145453692.png)



## 编译

```
make
```

windows

```powershell
.\build.bat
```



## 配置文件

| 名称          | 说明                                           |
| ------------- | ---------------------------------------------- |
| index_name    | 数据写入的索引名称                             |
| task_actions  | 任务监控的类型，对应es /_tasks接口actions匹配  |
| monitor_envs  | 任务监控那些环境，对应服务管理ES集群下节点名称 |
| write_data_es | 数据写入的es节点                               |

