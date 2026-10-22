package mailutil

import (
	"crypto/rand"
	"fmt"
	"math/big"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	dm "github.com/alibabacloud-go/dm-20151123/v2/client"
	"github.com/alibabacloud-go/tea/tea"
)

type Mail struct {
	client *dm.Client
	accountName *string
	subject *string
	fromAlias *string
}

// 构建DM客户端
func createDmClient(accessKeyId, accessKeySecret string) (*dm.Client, error) {
	config := &openapi.Config{
		AccessKeyId:     &accessKeyId,
		AccessKeySecret: &accessKeySecret,
		Endpoint:        tea.String("dm.aliyuncs.com"), // 国内区域固定这个endpoint
		ConnectTimeout: tea.Int(5000),
        ReadTimeout:    tea.Int(5000),
	}
	return dm.NewClient(config)
}

func InitMail(accessKeyId, accessKeySecret, accountName string) (*Mail, error) {
	client, err := createDmClient(accessKeyId, accessKeySecret)
	if err != nil {
		return nil, fmt.Errorf("create dm client fail: %w", err)
	}
	return &Mail{
		client: client,
		accountName: tea.String(accountName),
		subject: tea.String("研遇 LitStream - 邮箱验证码"),
		fromAlias: tea.String("研遇"),
	}, nil
}

// SendVerifyMail 发送邮箱验证码（触发邮件）
// toAddr:收件邮箱, code:验证码
func (m *Mail) send(htmlBody string, toAddr string) error {
	// 构建请求
	req := &dm.SingleSendMailRequest{
		AccountName:    m.accountName, // 你的发信地址，保持原样
		AddressType:    tea.Int32(1),
		ReplyToAddress: tea.Bool(false),
		ToAddress:      tea.String(toAddr),
		HtmlBody:       tea.String(htmlBody),
		Subject:        m.subject,
		FromAlias:      m.fromAlias,
	}

	resp, err := m.client.SingleSendMail(req)
	if err != nil {
		return fmt.Errorf("call SingleSendMail fail: %w", err)
	}
	fmt.Printf("发送成功！RequestId: %s\n", *resp.Body.RequestId)
	return nil
}

func (m *Mail) Send(toAddr string, isSignUp bool) (string, error) {
	code, err := Gen6Code()
	if err != nil {
		return "", err
	}
	var htmlBody string
	if isSignUp{
		htmlBody = SginUpHtmlBefore + code + SginUpHtmlAfter
	} else {
		htmlBody = ForgetPasswordHtmlBefore + code + ForgetPasswordHtmlAfter
	}
	return code, m.send(htmlBody, toAddr)
}

func Gen6Code() (string, error) {
	max := big.NewInt(900000)
	// rand.Int(reader, max) 返回 [0,max)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	num := n.Int64() + 100000
	return fmt.Sprintf("%06d", num), nil
}