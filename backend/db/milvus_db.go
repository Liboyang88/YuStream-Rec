package db

import (
	"context"
	"fmt"
	"litstream/domain"
	"strconv"
	"strings"
	"time"

	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

type MilvusDB struct {
	collectionName string
	vecDim         int
	hnswEF         int
	Cli            client.Client
}

const (
	// milvusStartupTimeout 启动期连接 Milvus 的总预算。
	// SDK 内部 grpc.WithBlock() 会让 DialContext 一直阻塞，必须靠 ctx 兜住。
	milvusStartupTimeout = 15 * time.Second

	// milvusMaxInTerms = 16384
)

func InitMilvus(env *domain.Env) (*MilvusDB, error) {
	collectionName := env.MilvusCollectionName
	vecDim := env.VecDim
	cxt, cancel := context.WithTimeout(context.Background(), milvusStartupTimeout)
	defer cancel()

	cli, err := client.NewClient(cxt, client.Config{
		Address: env.MilvusAddress,
	})

	if err != nil {
		return nil, fmt.Errorf("连接 Milvus(%s) 失败（%s 内未就绪）: %w",
			env.MilvusAddress, milvusStartupTimeout, err)
	}

	{
		fields := []*entity.Field{
			{Name: "id", DataType: entity.FieldTypeInt64, PrimaryKey: true, AutoID: false},
			{Name: "vec", DataType: entity.FieldTypeFloatVector,
				TypeParams: map[string]string{
					entity.TypeParamDim: fmt.Sprintf("%d", vecDim),
				}},
		}

		collSchema := &entity.Schema{
			CollectionName: collectionName,
			Fields:         fields,
		}

		exist, err := cli.HasCollection(cxt, collectionName)
		if err != nil {
			return nil, err
		}
		if !exist {
			err = cli.CreateCollection(cxt, collSchema, 1)
			if err != nil {
				return nil, err
			}
		}
	}
	{
		// 建索引：幂等——已有索引就不要重复 CreateIndex（重复创建会返回错误并让启动 panic）
		idxs, err := cli.DescribeIndex(cxt, collectionName, "vec")
		if err != nil || len(idxs) == 0 {
			indexParam, err := entity.NewIndexHNSW(entity.L2, 16, 64)
			if err != nil {
				return nil, err
			}
			err = cli.CreateIndex(cxt, collectionName, "vec", indexParam, false)
			if err != nil {
				return nil, fmt.Errorf("create index failed: %w", err)
			}
		}

		// 集合必须先加载进内存（query nodes）才能执行 search/query，
		// 否则会报 "collection not loaded"。async=false 表示等待加载完成。
		if state, err := cli.GetLoadState(cxt, collectionName, nil); err != nil || state != entity.LoadStateLoaded {
			if err := cli.LoadCollection(cxt, collectionName, false); err != nil {
				return nil, fmt.Errorf("load collection failed: %w", err)
			}
		}
	}

	return &MilvusDB{Cli: cli, collectionName: collectionName, vecDim: vecDim}, nil
}

func (m *MilvusDB) Close() error {
	return m.Cli.Close()
}

func (m *MilvusDB) QueryById(ctx context.Context, id uint) ([]float32, error) {

	filter := fmt.Sprintf("id in [%d]", int64(id))
	queryRes, err := m.Cli.Query(ctx, m.collectionName, nil, filter, []string{"vec"})
	if err != nil {
		return nil, err
	}

	if len(queryRes) == 0 {
		return nil, fmt.Errorf("id %d not found", id)
	}

	col, ok := queryRes[0].(*entity.ColumnFloatVector)
	if !ok {
		return nil, fmt.Errorf("id %d: 向量列类型异常 %T", id, queryRes[0])
	}
	data := col.Data()
	if len(data) == 0 {
		return nil, fmt.Errorf("id %d: 向量列为空", id)
	}
	out := make([]float32, len(data[0]))
	copy(out, data[0]) // 返回副本，避免把 SDK 内部缓冲区暴露给调用方
	return out, nil
}

func (m *MilvusDB) getHnswEF(topK int) int {
	ef := m.hnswEF
	if ef < topK*2 {
		ef = topK * 2
	}
	if ef < topK {
		ef = topK
	}
	return ef
}

func (m *MilvusDB) SearchVec(ctx context.Context, searchVec *[]float32, topK int, excludeIDs []uint) ([]uint, error) {
	if topK <= 0 {
		return nil, fmt.Errorf("topK 必须为正数, got %d", topK)
	}
	filter := ""
	if len(excludeIDs) > 0 {
		idsStr := make([]string, 0, len(excludeIDs))
		for _, id := range excludeIDs {
			idsStr = append(idsStr, strconv.FormatUint(uint64(id), 10))
		}
		filter = fmt.Sprintf("id not in [%s]", strings.Join(idsStr, ","))
	}
	// 检索参数：L2距离，返回Top2
	sp, err := entity.NewIndexHNSWSearchParam(m.getHnswEF(topK))
	if err != nil {
		return nil, err
	}
	result, err := m.Cli.Search(ctx,
		m.collectionName, // 集合名称
		[]string{},       // 分区列表，空表示全部分区
		filter,           // 标量过滤表达式
		[]string{"id"},   // 需要返回的标量字段
		[]entity.Vector{entity.FloatVector(*searchVec)}, // 查询向量数组（单向量则长度为1）
		"vec",     // 向量字段名
		entity.L2, // 度量类型，必须与索引一致
		topK,      // 返回TopK结果
		sp,        // 索引专属搜索参数)
	)
	if err != nil {
		return nil, fmt.Errorf("milvus search failed: %w", err)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("milvus search returned no result")
	}

	idColumn, ok := result[0].IDs.(*entity.ColumnInt64)

	if !ok {
		return nil, fmt.Errorf("id column not found")
	}

	return Int64ToUint(idColumn.Data())
}

func Int64ToUint(ids []int64) ([]uint, error) {
	res := make([]uint, 0, len(ids))
	for _, id := range ids {
		if id < 0 {
			return nil, fmt.Errorf("非法负ID: %d，无法转为无符号类型", id)
		}
		res = append(res, uint(id))
	}
	return res, nil
}
