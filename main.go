package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hekmon/transmissionrpc/v2"
)

// 默认连接参数
const (
	// 重试次数和超时时间设置
	MAX_RETRIES = 3
)

// 定义一个结构体用于存储合集和分集的映射关系
type DuplicateGroup struct {
	Collection      *transmissionrpc.Torrent   // 合集种子（较大的文件）
	Episodes        []*transmissionrpc.Torrent // 分集种子（较小的文件）
	HasFileOverlaps bool                       // 是否文件列表有重叠
}

// 用于识别剧集号的正则表达式
var episodeRegex = regexp.MustCompile(`[Ss](\d+)[Ee](\d+)`)

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

	// 输入种子名称筛选结尾
	fmt.Print("种子名称筛选结尾（多个以;分隔，直接回车则不筛选）[例如: ADWeb;HHWEB]: ")
	suffixesInput, _ := reader.ReadString('\n')
	suffixesInput = strings.TrimSpace(suffixesInput)
	var suffixFilters []string
	if suffixesInput != "" {
		suffixFilters = strings.Split(suffixesInput, ";")
		// 移除可能的空白
		for i, suffix := range suffixFilters {
			suffixFilters[i] = strings.TrimSpace(suffix)
		}
	}

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

	if len(suffixFilters) > 0 {
		fmt.Printf("种子名称筛选结尾: %s\n", strings.Join(suffixFilters, ", "))
	} else {
		fmt.Println("不进行种子名称筛选")
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

	// 筛选种子
	var filteredTorrents []transmissionrpc.Torrent
	if len(suffixFilters) > 0 {
		// 按名称结尾筛选
		for _, torrent := range torrents {
			if torrent.Name != nil {
				torrentName := *torrent.Name
				for _, suffix := range suffixFilters {
					if suffix != "" && strings.HasSuffix(torrentName, suffix) {
						filteredTorrents = append(filteredTorrents, torrent)
						break // 只要匹配一个后缀就添加
					}
				}
			}
		}

		if len(filteredTorrents) == 0 {
			fmt.Printf("未找到名称以 %s 结尾的种子\n", strings.Join(suffixFilters, ", "))
			return
		}

		fmt.Printf("找到 %d 个名称以 %s 结尾的种子\n",
			len(filteredTorrents), strings.Join(suffixFilters, ", "))
	} else {
		// 不筛选，使用所有种子
		filteredTorrents = torrents
		fmt.Printf("没有应用筛选，将处理所有 %d 个种子\n", len(torrents))
	}

	// 查找合集和分集关系
	fmt.Println("开始查找合集和分集关系...")
	duplicateGroups, dupGroupsWithOnlySameSize := findCollectionsAndEpisodes(client, filteredTorrents)

	// 显示有分集但大小相同的合集信息（仅记录）
	if len(dupGroupsWithOnlySameSize) > 0 {
		fmt.Printf("\n找到 %d 组只有大小相同分集的合集(这些不会被暂停):\n", len(dupGroupsWithOnlySameSize))
		for groupName, group := range dupGroupsWithOnlySameSize {
			fmt.Printf("\n组名: %s\n", groupName)

			// 显示合集信息
			if group.Collection != nil && group.Collection.ID != nil && group.Collection.SizeWhenDone != nil {
				collectionSize := (*group.Collection.SizeWhenDone).MB()
				fmt.Printf("合集(不会被暂停): ID: %d, 大小: %.2f MB\n", *group.Collection.ID, collectionSize)
			}

			// 显示大小相同分集信息
			if len(group.Episodes) > 0 {
				fmt.Printf("包含 %d 个大小相同分集(大小与合集一致):\n", len(group.Episodes))
				for i, episode := range group.Episodes {
					if episode != nil && episode.ID != nil && episode.SizeWhenDone != nil {
						episodeSize := (*episode.SizeWhenDone).MB()
						fmt.Printf("  %d. ID: %d, 大小: %.2f MB\n", i+1, *episode.ID, episodeSize)
					}
				}
			}

			// 显示文件重叠状态
			fmt.Printf("文件列表重叠状态: %t\n", group.HasFileOverlaps)
		}
	}

	if len(duplicateGroups) == 0 {
		fmt.Println("未找到需要处理的合集和对应分集的种子")
		return
	}

	// 显示找到的合集和分集信息
	fmt.Printf("找到 %d 组需要处理的合集和对应分集:\n", len(duplicateGroups))
	for groupName, group := range duplicateGroups {
		fmt.Printf("\n组名: %s\n", groupName)

		// 显示合集信息
		if group.Collection != nil && group.Collection.ID != nil && group.Collection.SizeWhenDone != nil {
			collectionSize := (*group.Collection.SizeWhenDone).MB()
			fmt.Printf("合集(不会被暂停): ID: %d, 大小: %.2f MB\n", *group.Collection.ID, collectionSize)

			// 显示合集的文件列表
			collectionFiles, err := getTorrentFiles(client, group.Collection.ID)
			if err == nil && len(collectionFiles) > 0 {
				fmt.Println("  合集文件列表:")
				for i, file := range collectionFiles {
					if i < 5 { // 最多显示5个文件
						fmt.Printf("    - %s\n", file.Name)
					} else {
						fmt.Printf("    - ... 以及 %d 个更多文件\n", len(collectionFiles)-5)
						break
					}
				}
			}
		}

		// 显示分集信息
		fmt.Printf("包含 %d 个分集(将被暂停):\n", len(group.Episodes))
		for i, episode := range group.Episodes {
			if episode != nil && episode.ID != nil && episode.SizeWhenDone != nil {
				episodeSize := (*episode.SizeWhenDone).MB()
				fmt.Printf("  %d. ID: %d, 大小: %.2f MB\n", i+1, *episode.ID, episodeSize)

				// 显示分集的文件列表
				episodeFiles, err := getTorrentFiles(client, episode.ID)
				if err == nil && len(episodeFiles) > 0 {
					fmt.Println("    文件列表:")
					for j, file := range episodeFiles {
						if j < 3 { // 最多显示3个文件
							fmt.Printf("      - %s\n", file.Name)
						} else {
							fmt.Printf("      - ... 以及 %d 个更多文件\n", len(episodeFiles)-3)
							break
						}
					}
				}
			}
		}

		// 显示文件重叠状态
		fmt.Printf("文件列表重叠状态: %t\n", group.HasFileOverlaps)
	}

	// 询问用户是否暂停这些种子
	fmt.Print("\n是否要暂停分集种子? (y/n): ")
	var answer string
	fmt.Scanln(&answer)

	if strings.ToLower(answer) != "y" {
		fmt.Println("操作已取消")
		return
	}

	// 暂停合集和分集种子
	successCount, failedCount := pauseEpisodes(client, duplicateGroups)
	fmt.Printf("\n操作完成: 成功暂停 %d 个分集, 失败 %d 个分集\n", successCount, failedCount)
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

// 查找合集和分集关系
func findCollectionsAndEpisodes(client *transmissionrpc.Client, torrents []transmissionrpc.Torrent) (map[string]DuplicateGroup, map[string]DuplicateGroup) {
	// 按名称分组
	nameGroups := make(map[string][]transmissionrpc.Torrent)
	for _, torrent := range torrents {
		if torrent.Name != nil {
			nameGroups[*torrent.Name] = append(nameGroups[*torrent.Name], torrent)
		}
	}

	// 查找合集和分集
	result := make(map[string]DuplicateGroup)
	onlySameSizeResult := make(map[string]DuplicateGroup)
	var processedCount, skippedCount, withoutEpisodesCount, sameSizeCount, onlySameSizeEpisodesCount, differentEpisodesCount int

	for name, group := range nameGroups {
		processedCount++
		if len(group) > 1 {
			// 检查所有种子大小是否相同
			allSameSizes := true
			var baseSize float64
			if group[0].SizeWhenDone != nil {
				baseSize = (*group[0].SizeWhenDone).Byte()
			}

			for i := 1; i < len(group); i++ {
				if group[i].SizeWhenDone != nil {
					currentSize := (*group[i].SizeWhenDone).Byte()
					// 如果发现大小不同（允许1KB以内的误差），标记为不同
					if abs(currentSize-baseSize) > 1024 {
						allSameSizes = false
						break
					}
				}
			}

			// 如果所有种子大小都相同，跳过这组种子
			if allSameSizes {
				fmt.Printf("跳过大小相同的种子组: %s (大小: %.2f MB)\n", name, baseSize/1024/1024)
				sameSizeCount++
				continue
			}

			// 排序：按大小从大到小排序（合集通常比分集大）
			var sortedGroup []transmissionrpc.Torrent = make([]transmissionrpc.Torrent, len(group))
			copy(sortedGroup, group)
			for i := 0; i < len(sortedGroup); i++ {
				for j := i + 1; j < len(sortedGroup); j++ {
					if sortedGroup[i].SizeWhenDone != nil && sortedGroup[j].SizeWhenDone != nil {
						sizeI := (*sortedGroup[i].SizeWhenDone).Byte()
						sizeJ := (*sortedGroup[j].SizeWhenDone).Byte()
						if sizeI < sizeJ {
							sortedGroup[i], sortedGroup[j] = sortedGroup[j], sortedGroup[i]
						}
					}
				}
			}

			// 检查文件列表包含关系
			if len(sortedGroup) >= 2 {
				// 假设最大的是合集
				collection := sortedGroup[0]
				var episodes []*transmissionrpc.Torrent
				var sameSizeEpisodes []*transmissionrpc.Torrent
				hasFileOverlaps := false

				// 获取合集的文件列表
				collectionFiles, err := getTorrentFiles(client, collection.ID)
				if err != nil {
					log.Printf("获取种子 ID: %d 文件列表失败: %v", *collection.ID, err)
					skippedCount++
					continue
				}

				// 获取合集大小
				var collectionSize float64
				if collection.SizeWhenDone != nil {
					collectionSize = (*collection.SizeWhenDone).Byte()
				}

				// 对每个可能的分集检查文件列表
				for i := 1; i < len(sortedGroup); i++ {
					episode := sortedGroup[i]
					episodeFiles, err := getTorrentFiles(client, episode.ID)
					if err != nil {
						log.Printf("获取种子 ID: %d 文件列表失败: %v", *episode.ID, err)
						continue
					}

					// 检查分集的大小
					var episodeSize float64
					if episode.SizeWhenDone != nil {
						episodeSize = (*episode.SizeWhenDone).Byte()
					}

					// 检查分集文件是否实际上是合集的一部分
					isActualEpisode, overlappingFiles := checkActualEpisodeOverlap(collectionFiles, episodeFiles)

					if isActualEpisode {
						hasFileOverlaps = true
						episodeCopy := episode // 创建副本以避免引用问题

						// 检查大小是否与合集相同
						if abs(episodeSize-collectionSize) <= 1024 {
							// 大小相同，不认为是需要处理的分集
							sameSizeEpisodes = append(sameSizeEpisodes, &episodeCopy)
						} else {
							// 大小不同，是需要处理的分集
							episodes = append(episodes, &episodeCopy)
						}
					} else if overlappingFiles > 0 {
						// 有重叠但不是真正的分集关系（可能是不同剧集）
						if collection.Name != nil && episode.Name != nil {
							fmt.Printf("跳过可能是不同剧集的种子: %s 和 %s (有 %d 个重叠文件)\n",
								*collection.Name, *episode.Name, overlappingFiles)
						}
						differentEpisodesCount++
					}
				}

				// 创建合集副本用于结果
				collectionCopy := collection

				// 只有当存在文件重叠时继续
				if hasFileOverlaps {
					// 分成两种情况：有真正的分集 和 只有大小相同的"分集"
					if len(episodes) > 0 {
						// 有真正的分集（大小不同），加入需要处理的结果
						result[name] = DuplicateGroup{
							Collection:      &collectionCopy,
							Episodes:        episodes,
							HasFileOverlaps: hasFileOverlaps,
						}
					} else if len(sameSizeEpisodes) > 0 {
						// 只有大小相同的"分集"，加入仅记录的结果
						onlySameSizeResult[name] = DuplicateGroup{
							Collection:      &collectionCopy,
							Episodes:        sameSizeEpisodes,
							HasFileOverlaps: hasFileOverlaps,
						}
						onlySameSizeEpisodesCount++
					} else {
						// 没有分集
						if collection.Name != nil {
							fmt.Printf("跳过没有分集的种子: %s\n", *collection.Name)
						}
						withoutEpisodesCount++
					}
				} else {
					// 记录没有找到分集的种子
					if collection.Name != nil {
						fmt.Printf("跳过没有分集的种子: %s\n", *collection.Name)
					}
					withoutEpisodesCount++
				}
			}
		} else {
			// 记录单种子的情况（不是名称重复的）
			if group[0].Name != nil {
				fmt.Printf("跳过单个种子: %s\n", *group[0].Name)
			}
			skippedCount++
		}
	}

	fmt.Printf("\n筛选统计：\n")
	fmt.Printf("- 处理种子组数量: %d\n", processedCount)
	fmt.Printf("- 跳过种子组数量: %d\n", skippedCount)
	fmt.Printf("- 跳过大小相同的种子组数量: %d\n", sameSizeCount)
	fmt.Printf("- 跳过不同剧集的种子组数量: %d\n", differentEpisodesCount)
	fmt.Printf("- 没有找到分集的种子组数量: %d\n", withoutEpisodesCount)
	fmt.Printf("- 只有大小相同分集的种子组数量: %d\n", onlySameSizeEpisodesCount)
	fmt.Printf("- 符合条件的种子组数量: %d\n", len(result))

	return result, onlySameSizeResult
}

// 检查是否真正的分集关系并返回重叠文件数量
func checkActualEpisodeOverlap(collectionFiles, episodeFiles []*transmissionrpc.TorrentFile) (bool, int) {
	// 如果文件数量不对，可能不是分集与合集的关系
	// 通常合集应该有更多的文件，或者至少等于分集文件数
	if len(collectionFiles) < len(episodeFiles) {
		return false, 0
	}

	// 检查重叠的文件
	var matchCount int
	var hasEpisodeMarker bool

	// 提取所有文件的剧集信息
	collectionEpisodes := make(map[string]bool)
	episodeEpisodes := make(map[string]bool)

	// 先检查是否存在剧集标识，如S01E01, S01E02等
	for _, file := range collectionFiles {
		epMarker := extractEpisodeMarker(file.Name)
		if epMarker != "" {
			collectionEpisodes[epMarker] = true
		}
	}

	for _, file := range episodeFiles {
		epMarker := extractEpisodeMarker(file.Name)
		if epMarker != "" {
			episodeEpisodes[epMarker] = true
			hasEpisodeMarker = true
		}
	}

	// 如果发现都有剧集标识，且标识完全不同，则不是合集与分集的关系
	if hasEpisodeMarker && len(collectionEpisodes) > 0 && len(episodeEpisodes) > 0 {
		// 检查是否有交集
		hasIntersection := false
		for marker := range episodeEpisodes {
			if collectionEpisodes[marker] {
				hasIntersection = true
				break
			}
		}

		// 如果没有交集，这些可能是不同的剧集，不是合集与分集的关系
		if !hasIntersection {
			// 记录有多少个重叠文件
			for _, episodeFile := range episodeFiles {
				for _, collectionFile := range collectionFiles {
					// 根据文件名（去掉路径和剧集标识）来比较
					episodeFileName := getFileName(episodeFile.Name)
					collectionFileName := getFileName(collectionFile.Name)

					if strings.Contains(episodeFileName, collectionFileName) ||
						strings.Contains(collectionFileName, episodeFileName) {
						matchCount++
						break
					}
				}
			}
			return false, matchCount
		}
	}

	// 常规文件对比
	for _, episodeFile := range episodeFiles {
		for _, collectionFile := range collectionFiles {
			// 根据文件名（去掉路径）来比较
			episodeFileName := getFileName(episodeFile.Name)
			collectionFileName := getFileName(collectionFile.Name)

			// 检查是否为完全匹配或合集包含分集
			if episodeFileName == collectionFileName ||
				strings.Contains(collectionFileName, episodeFileName) {
				matchCount++
				break
			}
		}
	}

	// 如果50%以上的分集文件在合集中找到，则认为有重叠
	return matchCount >= len(episodeFiles)/2, matchCount
}

// 提取文件名中的剧集标识（如S01E01）
func extractEpisodeMarker(filename string) string {
	matches := episodeRegex.FindStringSubmatch(filename)
	if len(matches) >= 3 {
		return matches[0] // 返回完整的匹配，如S01E01
	}
	return ""
}

// 计算绝对值
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// 获取种子的文件列表
func getTorrentFiles(client *transmissionrpc.Client, torrentID *int64) ([]*transmissionrpc.TorrentFile, error) {
	if torrentID == nil {
		return nil, fmt.Errorf("种子ID为空")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 获取种子详情，包含文件列表
	torrent, err := client.TorrentGet(ctx, []string{"files"}, []int64{*torrentID})
	if err != nil {
		return nil, err
	}

	if len(torrent) == 0 || torrent[0].Files == nil {
		return nil, fmt.Errorf("获取种子文件列表失败")
	}

	return torrent[0].Files, nil
}

// 从完整路径中获取文件名
func getFileName(path string) string {
	parts := strings.Split(path, "/")
	return parts[len(parts)-1]
}

// 只暂停分集种子，不暂停合集
func pauseEpisodes(client *transmissionrpc.Client, duplicateGroups map[string]DuplicateGroup) (int, int) {
	successCount := 0
	failedCount := 0

	for groupName, group := range duplicateGroups {
		// 只收集分集ID，不包括合集
		var torrentIDs []int64

		// 添加所有分集ID
		for _, episode := range group.Episodes {
			if episode != nil && episode.ID != nil {
				torrentIDs = append(torrentIDs, *episode.ID)
			}
		}

		// 暂停这些分集
		if len(torrentIDs) > 0 {
			fmt.Printf("正在暂停 \"%s\" 的 %d 个分集...\n", groupName, len(torrentIDs))

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			err := client.TorrentStopIDs(ctx, torrentIDs)
			cancel()

			if err == nil {
				successCount += len(torrentIDs)
				fmt.Printf("成功暂停 %d 个分集\n", len(torrentIDs))
			} else {
				failedCount += len(torrentIDs)
				fmt.Printf("暂停分集失败: %v\n", err)

				// 单独尝试暂停每个分集
				for _, id := range torrentIDs {
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					err := client.TorrentStopIDs(ctx, []int64{id})
					cancel()

					if err == nil {
						successCount++
						failedCount--
						fmt.Printf("成功暂停分集 ID: %d\n", id)
					} else {
						fmt.Printf("暂停分集 ID: %d 失败: %v\n", id, err)
					}

					time.Sleep(1 * time.Second)
				}
			}
		}
	}

	return successCount, failedCount
}
