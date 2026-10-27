# v0.1.1 — qB API Token 认证

- 增加 qB 5.2+/WebAPI 2.14.1+ 的原生 API Token（Authorization: Bearer）认证，不调用 login/logout；配置 Token 时优先使用，用户名/密码继续兼容旧版本。
- 增加加密保存、留空保持、显式清除及状态提示，不回显 Token，不写入日志或发布包。
- NAS 执行端支持只提供 Token 的 qB 配置；更新发布包版本读取和镜像标签。
- 保留全部配额、租约、跨月和发布恢复保护。81 保护保持不变；DNS 指向受保护服务器时不会自动发起连接。

原生认证规范：[qBittorrent 官方文档](https://github.com/qbittorrent/qBittorrent/wiki/API-Key-Authentication-%28%E2%89%A5v5.2.0%29)。真实版本测试与真实下载/发种分别记录，详见 VALIDATION.md。
