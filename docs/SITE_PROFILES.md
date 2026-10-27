# 通过JSON增加站点

在控制台「站点管理」下载模板，编辑后选择JSON文件，先校验预览，再确认导入。单个文件最多64 KiB，UTF-8编码，一个文件描述一个站点。也可下载JSON Schema供编辑器校验；仓库提供[模板](sites/template.json)和[Schema](sites/schema.json)。

第一版支持常见NexusPHP页面。模板声明解析方式，不执行Python/JavaScript，不包含Cookie、RSS密钥或密码。新的站点协议需要开发适配器；PTNexus也必须已支持所填写的站点映射。导入成功表示格式通过，实际站点仍需要验证。

关键字段：

|字段|用途|
|---|---|
|schema_version|固定为1|
|id|唯一小写ID，以字母开头，只允许字母、数字、下划线和短横线，最多64字符|
|name|页面显示名称|
|base_url|站点HTTPS域名，不含路径、凭据、查询参数或片段|
|roles|source、target或两者|
|engine|固定nexusphp|
|ptnexus_site|PTNexus中实际使用的站点代码；null时允许扫描，不能创建转种任务|
|official_groups|官组名称，按资源标题最后的组名识别；仍需通过转载限制检查|
|announce_hosts|允许的Tracker精确域名，必须填写后才能创建转种任务|
|paths|listing、detail、download、search，必须站内相对路径；detail/download必须且仅含一个{id}，search含一个{query}|
|selectors|table、detail_link、title、description、download_link、tags、peers，使用CSS选择器|

路径中的编号可以在路径或查询参数中，例如`/torrent/{id}`或`/details.php?id={id}`。NexusPHP查询会附加search_area（IMDb=4，标题=0）和search_mode=0；不支持这些语义的站点需要单独适配。解析、转载许可或查重结果不确定时跳过，不能用模板关闭这些检查。

站点凭据在对应站点卡片单独填写，保存时加密。留空保持原值，明确勾选清除才删除。RSS地址必须属于该站点域名。旧的「连接与执行端」织梦/NovaHD凭据继续作为内置站点默认值，自定义站点必须在「站点管理」配置。

导入后在「自动规则」选择来源站和目标站、保存，再手动扫描。来源和目标不能相同，月度额度在所有站点之间共用。每次扫描、任务和补种修复保存规则及两站配置快照；修改模板不会改变旧任务的目标。修改配置后，旧候选必须重新扫描。相同origin不能注册为另一个ID，站点ID和origin不可改；其他字段更新需要勾选同ID更新。

旧库自动迁移：原有候选、任务、发布证据、额度和ID归到zmpt→novahd，保留原关联。部署前需要一致性数据备份。新版NAS执行端为0.2.0；旧执行端不能领取新任务。已迁移的数据不能直接交给旧镜像，回滚必须使用升级前备份；如果升级后已有真实发布，应保留发布账本并修复前进。

受保护81服务器规则继续有效，域名解析和种子中的全部Tracker地址都会检查；拒绝webseed及其他不受支持的额外网络地址。实际qB和PTNexus自身的网络与站点配置也需要联调核对，JSON模板不能替代下游服务的访问限制。
