package domain

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// TestItemMatchesBackend 保证本包的 Item 与后端 domain.Item 逐列一致。
//
// 两者写的是同一张 MySQL 表：列名/gorm tag 漂移会导致写入或读出失败，而编译器
// 发现不了（两个模块互相不引用）。这里直接解析后端源码做对比，任何人改了后端
// Item 而忘记同步 searcher/domain/item.go，测试就会失败。
func TestItemMatchesBackend(t *testing.T) {
	const backendPath = "../../backend/domain/models_domain.go"

	want, err := backendItemColumns(backendPath)
	if err != nil {
		t.Skipf("跳过一致性检查（无法解析后端模型 %s）: %v", backendPath, err)
	}

	got := localItemColumns()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("searcher 的 Item 与后端 Item 不一致\nsearcher: %v\nbackend : %v", got, want)
	}
}

// localItemColumns 返回本包 Item 的"字段名 -> gorm tag"映射（只取带列定义的字段）。
func localItemColumns() map[string]string {
	columns := make(map[string]string)
	typ := reflect.TypeOf(Item{})
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		tag := string(field.Tag)
		if strings.Contains(tag, "column:") {
			columns[field.Name] = tag
		}
	}
	return columns
}

// backendItemColumns 解析后端源码，返回其 Item 结构体的"字段名 -> gorm tag"映射。
func backendItemColumns(path string) (map[string]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}

	columns := make(map[string]string)
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.TypeSpec)
		if !ok || spec.Name.Name != "Item" {
			return true
		}
		structType, ok := spec.Type.(*ast.StructType)
		if !ok {
			return true
		}
		for _, field := range structType.Fields.List {
			if field.Tag == nil || len(field.Names) == 0 {
				continue
			}
			tag, err := strconv.Unquote(field.Tag.Value)
			if err != nil || !strings.Contains(tag, "column:") {
				continue
			}
			for _, name := range field.Names {
				columns[name.Name] = tag
			}
		}
		return false
	})

	if len(columns) == 0 {
		return nil, parserError("未在后端源码中找到 Item 的列定义")
	}
	return columns, nil
}

type parserError string

func (e parserError) Error() string { return string(e) }

// TestIdentifier 唯一标识来自 Url，且只裁剪首尾空白。
func TestIdentifier(t *testing.T) {
	cases := []struct {
		name string
		item Item
		want string
	}{
		{"arXiv ID 原样返回", Item{Url: "http://arxiv.org/abs/2406.00001v1"}, "http://arxiv.org/abs/2406.00001v1"},
		{"裁剪首尾空白", Item{Url: "  http://arxiv.org/abs/2406.00001v1\n"}, "http://arxiv.org/abs/2406.00001v1"},
		{"空标识", Item{}, ""},
		{"标题不影响标识", Item{Title: "Some Title", Url: "id-1"}, "id-1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.item.Identifier(); got != tc.want {
				t.Errorf("Identifier() = %q, want %q", got, tc.want)
			}
		})
	}
}
