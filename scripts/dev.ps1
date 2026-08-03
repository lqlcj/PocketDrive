# PocketDrive 本地开发(PowerShell):启动后端 :8080
# 前端热更新另开一个终端:cd web; npm run dev  → 打开 http://127.0.0.1:5173
$env:POCKETDRIVE_ADDR = ":8080"
$env:POCKETDRIVE_DATA_DIR = "./data"
# 开发用固定密码,登录账号 admin / admin123(生产环境用 docker-compose 里的环境变量)
$env:POCKETDRIVE_ADMIN_PASSWORD = "admin123"
go run ./cmd/pocketdrive
