# fyaitg Chatbot 项目使用说明

## 项目简介

#

> [!TIP]
> **基于 new-api，one-api 聚合模型对话 Telegram 的机器人，操作方便，部署简单，动态拉取 API 所支持的所有模型（openai 官方 API 也支持）** <br>
> **动态获取 api 支持的所有模型** <br>
> **支持 AMD64/ARM64** <br>
> **镜像大小 17M，内存占用 10M** <br>
> **——By [drfyup](https://hstz.com)**

#

## 效果图  

![image](https://github.com/user-attachments/assets/5742ee38-324f-4afc-b67b-11758d289777)![image](https://github.com/user-attachments/assets/1e9d3515-920f-4129-9230-129f41e59d5e)



## 功能说明

本项目提供了以下主要功能：

1. **与用户进行 AI 对话**: 机器人通过 Telegram 接收用户消息，并将其发送给 OpenAI API 进行处理，然后返回生成的文本。
2. **多轮对话**: 机器人能够记住之前的对话，提供连续对话的上下文支持，并能设置最大对话轮数。
3. **指定使用的 OpenAI 模型**: 支持从多个 OpenAI 模型中选择当前使用的模型，包含默认模型的配置。
4. **消息历史管理**: 支持清除当前会话历史，保持对话上下文清晰可控。
5. **权限管理**: 通过配置文件，可以限制允许与机器人交互的用户和频道。
6. **日志记录**: 记录详细的操作日志，包括消息收发、API 请求和错误等信息，便于排查问题和审计。

## Docker 和 Docker Compose 的部署说明

### 1. 安装 Docker 和 Docker Compose

如果您的系统尚未安装 Docker 和 Docker Compose，请使用以下命令简便地进行安装。

#### 安装 Docker

```bash

curl -fsSL https://get.docker.com -o get-docker.sh
sudo sh get-docker.sh
```
安装 Docker Compose
在安装 Docker 后，您可以使用以下命令安装 Docker Compose：


```bash

sudo curl -L "https://github.com/docker/compose/releases/download/v2.15.0/docker-compose-$(uname -s)-$(uname -m)" -o /usr/local/bin/docker-compose
sudo chmod +x /usr/local/bin/docker-compose
```

你可以通过运行以下命令确认 Docker 和 Docker Compose 已正确安装：

```bash

docker --version
docker-compose --version
```

2. 目录结构和使用方式
   在系统任意目录克隆仓库到本地并进入项目根目录：

```bash

git clone https://github.com/3377/fyaitg.git
cd fyaitg
```

项目的目录结构如下：

```plaintext

.
├── main.go                       # 程序入口文件
├── config/
│   └── config.yaml               # 配置文件
├── Dockerfile                    # Docker镜像描述文件
├── docker-compose.yaml           # Docker Compose配置文件
└── README.md                     # 项目说明文档
```

3. 修改配置文件
   在启动 Docker 容器之前，请确保编辑 config/config.yaml 文件以包含正确的 Telegram Token 和 OpenAI 的 API Key，并根据需要配置其他参数。
   示例如下：

```yaml
telegram_token: ""
openai_config:
  api_key: ""
  api_url: "" # 类似 https://api.openai.com/v1，写到v1截止
default_model: "gpt-4o-mini" # 初始化模型，不写没关系，动态获取后直接选择即可
system_prompt: "基于中文对话"  # 系统提示词配置
history_length: 10 # 保存的最近对话轮数
history_timeout_minutes: 30 # 对话保留时间，单位：分钟
allowed_users:
  - tg号  # Telegram用户ID
allowed_channels:
  - "频道号"  # 允许的Telegram频道名称
```

4. 启动项目
   该项目已在 Docker Hub 上构建并发布，仓库及镜像名为 drfyup/fyaitg:latest。您可以直接使用以下步骤快速启动项目：

使用 Docker 启动
如果您希望以最小的方式启动容器，可以执行以下命令：

```bash

docker run -d --name telegram-bot -v $(pwd)/config:/app/config -p 8000:8000 drfyup/fyaitg:latest
```

这个命令将启动机器人，并挂载本地的配置文件目录到容器内部的 /app/config 路径，确保配置文件能够被读取并使用。外部端口 8000 暴露出来以便进行健康检查（可选）。

使用 Docker Compose 启动
如果您希望使用 Docker Compose 进行部署，则可以直接使用已经准备好的 docker-compose.yaml 文件。执行以下命令：

```bash

docker-compose up -d
```

这个命令会根据 docker-compose.yaml 文件的描述启动应用，并在后台运行。默认情况下使用的是 drfyup/fyaitg:latest 镜像，不需要手动构建。

如果需要停止运行：

```bash

docker-compose down
```

5. 更新说明
   该项目在 Docker Hub 上持续集成更新简化了更新流程。当有新的版本发布时，您只需通过以下步骤进行更新：

使用 Docker 更新
拉取最新版本镜像 :

```bash

docker pull drfyup/fyaitg:latest
```

重启容器 :

```bash

docker stop telegram-bot
docker rm telegram-bot
docker run -d --name telegram-bot -v $(pwd)/config:/app/config -p 8000:8000 drfyup/fyaitg:latest
```

使用 Docker Compose 更新
直接拉取最新的镜像并重新启动服务：

```bash

docker-compose pull
docker-compose up -d --force-recreate
```

在重新启动之前, 请确保您已正确配置了 config.yaml 文件。

注：配置更新
如果配置文件 config.yaml 发生了变化，可以直接在编辑后进行服务刷新，使用 Docker Compose 执行 docker-compose restart 即可。
