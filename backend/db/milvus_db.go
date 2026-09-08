package db

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"

	"dcpro/domain"
)

type MilvusDB struct {
	collectionName string
	vecDim int
	Cli client.Client
}

func InitMilvus(env *domain.Env) (*MilvusDB, error) {
	collectionName := env.MilvusCollectionName
	vecDim := env.VecDim
	cxt := context.Background()
	cli, err := client.NewClient(cxt, client.Config{
		Address: env.MilvusAddress,
	})

	if err != nil {
		return nil, err
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
			Fields: fields,
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
		indexParam, err := entity.NewIndexHNSW(entity.L2, 16, 64)
		if err != nil {
			return nil, err
		}
		err = cli.CreateIndex(cxt, collectionName, "vec", indexParam, false)
		if err != nil {
			return nil, err
		}
		// 集合必须先加载进内存（query nodes）才能执行 search/query，
		// 否则会报 "collection not loaded"。async=false 表示等待加载完成。
		if err := cli.LoadCollection(cxt, collectionName, false); err != nil {
			return nil, fmt.Errorf("load collection failed: %w", err)
		}
	}

	return &MilvusDB{Cli: cli, collectionName: collectionName, vecDim: vecDim}, nil
}

func (m *MilvusDB) Close() error {
	return m.Cli.Close()
}

func (m *MilvusDB) QueryById(ctx context.Context, id uint) (*[]float32, error) {

	filter := fmt.Sprintf("id in [%d]", int64(id))
	queryFields := []string{"vec"}

	queryRes, err := m.Cli.Query(ctx, m.collectionName, nil, filter, queryFields)

	if err != nil {
		return nil, err
	}

	if len(queryRes) == 0 {
		return nil, fmt.Errorf("id %d not found", id)
	}

	vec := &queryRes[0].(*entity.ColumnFloatVector).Data()[0]
	return vec, nil
}

func (m *MilvusDB) SearchVec(ctx context.Context, searchVec *[]float32, topK int, excludeIDs []uint) ([]uint, error) {
	filter := ""
	if len(excludeIDs) > 0 {
		idsStr := make([]string, 0, len(excludeIDs))
		for _, id := range excludeIDs {
			idsStr = append(idsStr, strconv.FormatUint(uint64(id), 10))
		}
		filter = fmt.Sprintf("id not in [%s]", strings.Join(idsStr, ","))
	}
	// 检索参数：L2距离，返回Top2
	sp, err := entity.NewIndexHNSWSearchParam(64)
	if err != nil {
		return nil, err
	}
	result, err := m.Cli.Search(ctx, 
		m.collectionName,               // 集合名称
		[]string{},                   // 分区列表，空表示全部分区
		filter,                           // 标量过滤表达式
		[]string{"id"},// 需要返回的标量字段
		[]entity.Vector{entity.FloatVector(*searchVec)}, // 查询向量数组（单向量则长度为1）
		"vec",                  // 向量字段名
		entity.L2,                    // 度量类型，必须与索引一致
		topK,                         // 返回TopK结果
		sp,                 // 索引专属搜索参数)
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