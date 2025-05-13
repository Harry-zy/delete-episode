package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hekmon/transmissionrpc/v2"
)

// 默认连接参数
const (
	// 重试次数和超时时间设置
	MAX_RETRIES = 3
	// 种子名称筛选结尾
	NAME_SUFFIX_FILTER_1 = "ADWeb"
	NAME_SUFFIX_FILTER_2 = "HHWEB"
)

func main() {
	reader := bufio.NewReader(os.Stdin)

	// 提示用户输入连接参数
	fmt.Println("请输入Transmission服务器连接参数：")

	// 输入服务器地址
	fmt.Print("服务器地址 [默认: 127.0.0.1]: ")
	serverAddressInput, _ := reader.ReadString('\n')
	serverAddressInput = strings.TrimSpace(serverAddressInput)
	serverAddress := "127.0.0.1"
	if serverAddressInput != "" {
		serverAddress = serverAddressInput
	}

	// 输入端口
	fmt.Print("端口 [默认: 9091]: ")
	portInput, _ := reader.ReadString('\n')
	portInput = strings.TrimSpace(portInput)
	port := 9091
	if portInput != "" {
		portValue, err := strconv.Atoi(portInput)
		if err == nil && portValue > 0 {
			port = portValue
		} else {
			fmt.Println("端口输入无效，将使用默认值 9091")
		}
	}

	// 是否使用HTTPS
	fmt.Print("是否使用HTTPS (y/n) [默认: n]: ")
	httpsInput, _ := reader.ReadString('\n')
	httpsInput = strings.TrimSpace(httpsInput)
	isHttps := false
	if strings.ToLower(httpsInput) == "y" {
		isHttps = true
	}

	// 输入用户名
	fmt.Print("用户名 [默认: \"\"]: ")
	username, _ := reader.ReadString('\n')
	username = strings.TrimSpace(username)

	// 输入密码
	fmt.Print("密码 [默认: \"\"]: ")
	password, _ := reader.ReadString('\n')
	password = strings.TrimSpace(password)

	// 显示连接信息给用户确认
	fmt.Println("将使用以下连接参数:")
	fmt.Printf("服务器地址: %s\n", serverAddress)
	fmt.Printf("端口: %d\n", port)
	fmt.Printf("HTTPS: %t\n", isHttps)
	fmt.Printf("用户名: %s\n", username)
	if password != "" {
		fmt.Printf("密码: ******\n")
	} else {
		fmt.Printf("密码: \n")
	}

	// 确认连接参数
	fmt.Print("确认使用以上参数？(y/n) [默认: y]: ")
	confirmInput, _ := reader.ReadString('\n')
	confirmInput = strings.TrimSpace(confirmInput)
	if confirmInput != "" && strings.ToLower(confirmInput) != "y" {
		fmt.Println("已取消操作")
		return
	}

	// 创建一个 Transmission 客户端
	client, err := transmissionrpc.New(serverAddress, username, password, &transmissionrpc.AdvancedConfig{
		Port:  uint16(port),
		HTTPS: isHttps,
	})
	if err != nil {
		log.Fatalf("无法连接到 Transmission 服务器: %v", err)
	}

	// 获取所有 torrent
	torrents, err := getWithRetry(client)
	if err != nil {
		log.Fatalf("获取 torrent 列表失败: %v", err)
	}

	// 筛选出名称以ADWeb或HHWEB结尾的种子
	var filteredTorrents []transmissionrpc.Torrent
	for _, torrent := range torrents {
		if torrent.Name != nil && (strings.HasSuffix(*torrent.Name, NAME_SUFFIX_FILTER_1) ||
			strings.HasSuffix(*torrent.Name, NAME_SUFFIX_FILTER_2)) {
			filteredTorrents = append(filteredTorrents, torrent)
		}
	}

	if len(filteredTorrents) == 0 {
		fmt.Printf("未找到名称以 %s 或 %s 结尾的种子\n", NAME_SUFFIX_FILTER_1, NAME_SUFFIX_FILTER_2)
		return
	}

	fmt.Printf("找到 %d 个名称以 %s 或 %s 结尾的种子\n", len(filteredTorrents), NAME_SUFFIX_FILTER_1, NAME_SUFFIX_FILTER_2)

	// 在筛选的种子中查找名称相同且大小不同的种子
	duplicates := findDuplicatesByName(filteredTorrents)
	if len(duplicates) == 0 {
		fmt.Println("未找到名称相同且大小不同的种子")
		return
	}

	// 显示找到的重复种子信息
	fmt.Printf("找到 %d 组名称相同但大小不同的种子:\n", len(duplicates))
	for groupName, group := range duplicates {
		fmt.Printf("\n组名: %s\n", groupName)
		fmt.Printf("包含 %d 个种子:\n", len(group))
		for i, torrent := range group {
			if torrent.ID != nil && torrent.Name != nil && torrent.SizeWhenDone != nil {
				// 将大小转换为MB显示
				sizeInMB := (*torrent.SizeWhenDone).MB()
				fmt.Printf("  %d. ID: %d, 大小: %.2f MB\n", i+1, *torrent.ID, sizeInMB)
			}
		}
	}

	// 询问用户是否暂停这些种子
	fmt.Print("\n是否要暂停这些种子? (y/n): ")
	var answer string
	fmt.Scanln(&answer)

	if strings.ToLower(answer) != "y" {
		fmt.Println("操作已取消")
		return
	}

	// 暂停重复的种子
	successCount, failedCount := pauseDuplicateTorrents(client, duplicates)
	fmt.Printf("\n操作完成: 成功暂停 %d 个种子, 失败 %d 个种子\n", successCount, failedCount)
}

// 带重试的获取种子列表
func getWithRetry(client *transmissionrpc.Client) ([]transmissionrpc.Torrent, error) {
	var torrents []transmissionrpc.Torrent
	var err error

	for retry := 0; retry < MAX_RETRIES; retry++ {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		torrents, err = client.TorrentGetAll(ctx)
		cancel()

		if err == nil {
			return torrents, nil
		}

		log.Printf("获取种子列表失败，尝试重试 (%d/%d): %v", retry+1, MAX_RETRIES, err)
		time.Sleep(5 * time.Second)
	}

	return torrents, err
}

// 查找名称相同但大小不同的种子
func findDuplicatesByName(torrents []transmissionrpc.Torrent) map[string][]transmissionrpc.Torrent {
	// 先按名称分组
	nameGroups := make(map[string][]transmissionrpc.Torrent)
	for _, torrent := range torrents {
		if torrent.Name != nil {
			nameGroups[*torrent.Name] = append(nameGroups[*torrent.Name], torrent)
		}
	}

	// 筛选出名称相同但大小不同的种子组
	result := make(map[string][]transmissionrpc.Torrent)
	for name, group := range nameGroups {
		if len(group) > 1 {
			// 检查组内是否存在大小不同的种子
			hasDifferentSizes := false
			var baseSize float64

			if group[0].SizeWhenDone != nil {
				baseSize = (*group[0].SizeWhenDone).Byte()
			}

			for i := 1; i < len(group); i++ {
				if group[i].SizeWhenDone != nil && (*group[i].SizeWhenDone).Byte() != baseSize {
					hasDifferentSizes = true
					break
				}
			}

			if hasDifferentSizes {
				result[name] = group
			}
		}
	}

	return result
}

// 暂停重复的种子
func pauseDuplicateTorrents(client *transmissionrpc.Client, duplicates map[string][]transmissionrpc.Torrent) (int, int) {
	successCount := 0
	failedCount := 0

	for _, group := range duplicates {
		// 对每组重复种子，创建暂停请求
		var torrentIDs []int64
		for _, torrent := range group {
			if torrent.ID != nil {
				torrentIDs = append(torrentIDs, *torrent.ID)
			}
		}

		// 暂停这些种子
		if len(torrentIDs) > 0 {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			err := client.TorrentStopIDs(ctx, torrentIDs)
			cancel()

			if err == nil {
				successCount += len(torrentIDs)
				fmt.Printf("成功暂停 %d 个种子\n", len(torrentIDs))
			} else {
				failedCount += len(torrentIDs)
				fmt.Printf("暂停种子失败: %v\n", err)

				// 单独尝试暂停每个种子
				for _, id := range torrentIDs {
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					err := client.TorrentStopIDs(ctx, []int64{id})
					cancel()

					if err == nil {
						successCount++
						failedCount--
						fmt.Printf("成功暂停种子 ID: %d\n", id)
					} else {
						fmt.Printf("暂停种子 ID: %d 失败: %v\n", id, err)
					}

					time.Sleep(1 * time.Second)
				}
			}
		}
	}

	return successCount, failedCount
}
