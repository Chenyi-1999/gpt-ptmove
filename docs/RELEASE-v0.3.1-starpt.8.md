# STARPT v0.3.1-starpt.8

- 「任务记录 → 编辑已发布 → 更换封面」支持 Pixhost 展示页和图片直链。STARPT 解析原图，预览并确认后只更新封面。
- 任务记录的操作分为两行，窄屏中状态和操作分别显示。
- 已在 152 验证 Linux AMD64 镜像及 #226481 的只读封面预览。

## 使用 Compose 部署

GitHub 私有发行页提供完整 Linux AMD64 镜像、SHA-256 校验文件和 Compose。 [GitHub 发行页](https://github.com/Chenyi-1999/gpt-ptmove/releases/tag/v0.3.1-starpt.8)

Gitee 发行页提供 Compose 和 GitHub 镜像链接。Gitee 的仓库附件配额限制了镜像上传。 [Gitee 发行页](https://gitee.com/star1999/gpt-ptmove/releases/tag/v0.3.1-starpt.8)

把 `compose.starpt.yaml` 与下载的镜像文件放在同一目录，然后执行下方命令。

```sh
docker load -i starpt-v0.3.1-starpt.8-linux-amd64.tar.gz
docker compose -f compose.starpt.yaml up -d
```

默认管理端口为 `18120`，数据保存在 Compose 所在目录的 `starpt-data`。首次安装可用 `docker compose -f compose.starpt.yaml logs starpt` 查看初始化登录提示。可通过 `PTMOVE_BIND`、`PTMOVE_PORT` 或 `PTMOVE_DATA_DIR` 调整监听地址、端口和数据目录。可选的 `.env.ptmove` 可传入自己的运行配置。

完整镜像 SHA-256 为 `900019a17ada63d2160d83a1f716fcdf35c69f97d0815569a1ecf7df4c7f494a`。
