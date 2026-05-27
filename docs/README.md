# Mini Cloud Docs

这里放 `mini-cloud` 的版本化文档。

当前规则是：

- 一个大版本放在一个目录里
- 例如：
  - `v1/`
  - `v2/`
- 每个版本内部再按编号文件推进章节
  - 例如：
    - `01-product-scope-and-resource-model.md`
    - `02-control-plane-skeleton.md`
- 如果某个版本的 `README.md`
  - 已经承担总览和导航作用
  - 那么正式章节也可以直接从：
    - `01`
    - 开始

如果某一章后面真的需要：

- 附图
- 额外素材
- 多个配套文件

再单独给那一章开目录。

当前已经有：

- `docs/v1/`
- `docs/v2/`
- `docs/v3/`
- `docs/v4/`
- `docs/v5/`
- `docs/v6/`
- `docs/v7/`

路线图现在单独拆到了：

- `projects/mini-cloud/docs/ROADMAP.md`
  - 总路线图
- `projects/mini-cloud/docs/v1/ROADMAP.md`
- `projects/mini-cloud/docs/v2/ROADMAP.md`
- `projects/mini-cloud/docs/v3/ROADMAP.md`
- `projects/mini-cloud/docs/v4/ROADMAP.md`
- `projects/mini-cloud/docs/v5/ROADMAP.md`
- `projects/mini-cloud/docs/v6/ROADMAP.md`

从
`v7`
开始，
源码重构阶段不先写完整
`ROADMAP.md`，
只在：

- `projects/mini-cloud/docs/v7/README.md`

里记录重构原则和检查点。
