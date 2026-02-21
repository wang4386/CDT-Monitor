# Tailwind CSS v4 预编译脚本
# 使用方法: 在修改 Tailwind class 后运行此脚本重新编译 CSS
# 需要先安装 Node.js

Write-Host "正在编译 Tailwind CSS..." -ForegroundColor Cyan

node "C:\Program Files\nodejs\node_modules\npm\bin\npx-cli.js" @tailwindcss/cli@4 -i static/input.css -o static/tailwind-compiled.css --minify

if ($LASTEXITCODE -eq 0) {
    $size = (Get-Item static/tailwind-compiled.css).Length
    Write-Host "编译完成! 输出文件: static/tailwind-compiled.css ($size bytes)" -ForegroundColor Green
} else {
    Write-Host "编译失败!" -ForegroundColor Red
}
