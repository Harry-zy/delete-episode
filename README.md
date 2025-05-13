# delete-episode

删除重复种子工具 - 查找Transmission中名称相同但大小不同的种子并暂停它们

## 功能

这个工具可以帮助您：

1. 连接到Transmission服务器（支持自定义连接参数）
2. 筛选名称以ADWeb或HHWEB结尾的种子
3. 扫描这些种子，查找名称相同但大小不同的种子（可能是重复下载的不同版本）
4. 显示找到的重复种子组和它们的大小
5. 可选择暂停这些重复种子

## 使用方法

1. 编译程序：
   ```
   go build -o delete-episode
   ```

2. 运行程序：
   ```
   ./delete-episode
   ```

3. 按照提示输入Transmission服务器的连接参数：
   - 服务器地址（默认: 127.0.0.1）
   - 端口（默认: 9091）
   - 是否使用HTTPS（默认: n）
   - 用户名（默认为空）
   - 密码（默认为空）

4. 确认连接参数后，程序会：
   - 首先查找所有名称以ADWeb或HHWEB结尾的种子
   - 然后在这些种子中查找名称相同但大小不同的种子
   - 显示找到的重复种子组
   
5. 根据提示输入y/n决定是否暂停找到的重复种子

## 注意事项

1. 程序只处理名称以ADWeb或HHWEB结尾的种子
2. 暂停操作可以帮助您决定保留哪个版本的种子，避免重复下载占用空间
3. 如果连接失败，请检查您的连接参数是否正确

## 配置

在main.go文件中修改以下常量以适配您的Transmission服务器：

```go
// 固定的连接参数
const (
    SERVER_ADDRESS = "xxx.xxx.xxx.xxx"  // 您的Transmission服务器地址
    PORT           = 1234               // Transmission RPC端口
    IS_HTTPS       = true/false              // 是否使用HTTPS
    USERNAME       = "xxxxxxxx"         // RPC用户名
    PASSWORD       = "xxxxxxxx"           // RPC密码
    MAX_RETRIES    = 3                  // 重试次数
)
``` 