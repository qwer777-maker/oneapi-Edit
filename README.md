## 自用

## 基于one-api项目

## openai包重构

### 原项目问题

one-api是非常好的项目，大大便利了key的分发使用。现在遇到一些站点请求和返回的数据包是非openai标准格式，因此需要自定义url、请求头、返回包。

### 修改内容

1、自定义完整url

2、从.env文件自定义请求头

3、支持sse响应

后续会添加在前端直接修改请求头。

### 环境安装 linuxamd64

```
## go >=1.21
wget https://go.dev/dl/go1.24.1.linux-amd64.tar.gz
tar -xf go1.24.1.linux-amd64.tar.gz -C /usr/local/
echo "export PATH=$PATH:/usr/local/go/bin" >> ~/.bashrc
echo "export CGO_ENABLED=1" >> ~/.bashrc
source ~/.bashrc 
apt install build-essential
go env -w CGO_ENABLED=1

## npm
sudo apt update
sudo apt upgrade
sudo apt install -y build-essential curl
curl -fsSL https://deb.nodesource.com/setup_20.x | sudo -E bash -
sudo apt install -y nodejs
node -v     # 检查当前使用的 Node.js 版本
npm -v      # 确认 npm 已正确更新并工作正常



```



### 源码部署

1. clone源码

```
git clone https://github.com/Klearcc/oneapi-Edit.git

# 构建前端
cd oneapi-Edit/web/default
npm install
npm run build

# 构建后端
cd ../..
go mod download
go build -ldflags "-s -w" -o one-api
```

2. 

```
chmod u+x one-api
./one-api --port 3000 --log-dir ./logs
```

3. 访问 http://localhost:3000/ 并登录。初始账号用户名为 `root`，密码为 `123456`。

4. 更改源码后重载

```
go build -ldflags "-s -w" -o one-api
./one-api --port 3000 --log-dir ./logs
```


