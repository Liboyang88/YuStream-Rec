package mailutil

const SginUpHtmlBefore = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>研遇 LitStream - 验证码通知</title>
    <style>
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
            font-family: "Microsoft Yahei", PingFang SC, Helvetica Neue, Arial, sans-serif;
        }
        body {
            background-color: #f5f7fa;
            padding: 20px;
        }
        .email-container {
            max-width: 520px;
            margin: 0 auto;
            background: #ffffff;
            border-radius: 16px;
            box-shadow: 0 4px 20px rgba(0, 0, 0, 0.06);
            overflow: hidden;
        }
        .email-header {
            background: linear-gradient(135deg, #2563eb 0%, #7c3aed 100%);
            padding: 24px;
            text-align: center;
        }
        .email-header h2 {
            color: #ffffff;
            font-size: 22px;
            font-weight: 600;
            letter-spacing: 1px;
        }
        .email-body {
            padding: 36px 30px;
            color: #333333;
        }
        .email-tip {
            font-size: 15px;
            color: #000000;
            line-height: 1.8;
            margin-bottom: 13px;
        }
        .code-box {
            background-color: #f8fafc;
            border: 1px dashed #cbd5e1;
            border-radius: 12px;
            padding: 20px;
            text-align: center;
            margin: 28px 0;
        }
        .verify-code {
            font-size: 32px;
            font-weight: 700;
            color: #2563eb;
            letter-spacing: 6px;
        }
        .code-desc {
            font-size: 13px;
            color: #888;
            margin-top: 12px;
        }
        .notice-text {
            font-size: 14px;
            color: #666;
            line-height: 1.8;
            margin-top: 16px;
        }
        .warn-text {
            color: #f59e0b;
            font-weight: 500;
        }
        .email-footer {
            padding: 20px 30px;
            border-top: 1px solid #f1f5f9;
            text-align: center;
            font-size: 12px;
            color: #999;
            line-height: 1.6;
        }
    </style>
</head>
<body>
    <div class="email-container">
        <!-- 邮件头部 -->
        <div class="email-header">
            <h2>研遇 LitStream</h2>
        </div>

        <!-- 邮件主体内容 -->
        <div class="email-body">
            <p class="email-tip">您好！</p>
            <p class="email-tip" style="text-indent: 2em;">您正在进行【研遇 LitStream】<strong>账号注册</strong>操作，本次操作的动态验证码如下，请在有效期内完成验证。</p>

            <!-- 验证码展示区域 -->
            <div class="code-box">
                <div class="verify-code">`

const SginUpHtmlAfter = `</div>
                <div class="code-desc">5分钟内有效，请勿泄露给他人</div>
            </div>

            <p class="notice-text"> - 验证码仅用于本次身份验证，过期自动失效；</p>
            <p class="notice-text"> - 如非您本人操作，请立即修改账号密码，并忽略本邮件；</p>
        </div>

        <!-- 邮件底部声明 -->
        <div class="email-footer">
            <p>本邮件由系统自动发送，请勿直接回复</p>
        </div>
    </div>
</body>
</html>`

const ForgetPasswordHtmlBefore = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>研遇 LitStream - 验证码通知</title>
    <style>
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
            font-family: "Microsoft Yahei", PingFang SC, Helvetica Neue, Arial, sans-serif;
        }
        body {
            background-color: #f5f7fa;
            padding: 20px;
        }
        .email-container {
            max-width: 520px;
            margin: 0 auto;
            background: #ffffff;
            border-radius: 16px;
            box-shadow: 0 4px 20px rgba(0, 0, 0, 0.06);
            overflow: hidden;
        }
        .email-header {
            background: linear-gradient(135deg, #2563eb 0%, #7c3aed 100%);
            padding: 24px;
            text-align: center;
        }
        .email-header h2 {
            color: #ffffff;
            font-size: 22px;
            font-weight: 600;
            letter-spacing: 1px;
        }
        .email-body {
            padding: 36px 30px;
            color: #333333;
        }
        .email-tip {
            font-size: 15px;
            color: #000000;
            line-height: 1.8;
            margin-bottom: 13px;
        }
        .code-box {
            background-color: #f8fafc;
            border: 1px dashed #cbd5e1;
            border-radius: 12px;
            padding: 20px;
            text-align: center;
            margin: 28px 0;
        }
        .verify-code {
            font-size: 32px;
            font-weight: 700;
            color: #2563eb;
            letter-spacing: 6px;
        }
        .code-desc {
            font-size: 13px;
            color: #888;
            margin-top: 12px;
        }
        .notice-text {
            font-size: 14px;
            color: #666;
            line-height: 1.8;
            margin-top: 16px;
        }
        .warn-text {
            color: #f59e0b;
            font-weight: 500;
        }
        .email-footer {
            padding: 20px 30px;
            border-top: 1px solid #f1f5f9;
            text-align: center;
            font-size: 12px;
            color: #999;
            line-height: 1.6;
        }
    </style>
</head>
<body>
    <div class="email-container">
        <!-- 邮件头部 -->
        <div class="email-header">
            <h2>研遇 LitStream</h2>
        </div>

        <!-- 邮件主体内容 -->
        <div class="email-body">
            <p class="email-tip">您好！</p>
            <p class="email-tip" style="text-indent: 2em;">您正在进行【研遇 LitStream】<strong>密码找回</strong>操作，本次操作的动态验证码如下，请在有效期内完成验证。</p>

            <!-- 验证码展示区域 -->
            <div class="code-box">
                <div class="verify-code">`

const ForgetPasswordHtmlAfter = `</div>
                <div class="code-desc">5分钟内有效，请勿泄露给他人</div>
            </div>

            <p class="notice-text"> - 验证码仅用于本次身份验证，过期自动失效；</p>
            <p class="notice-text"> - 如非您本人操作，请立即修改账号密码，并忽略本邮件；</p>
        </div>

        <!-- 邮件底部声明 -->
        <div class="email-footer">
            <p>本邮件由系统自动发送，请勿直接回复</p>
        </div>
    </div>
</body>
</html>`
