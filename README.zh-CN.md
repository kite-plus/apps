<h1 align="center">Kite Apps</h1>

<p align="center">
  Kite 按名字安装的主题和插件。
</p>

<p align="center">
  <a href="https://github.com/kite-plus/apps/actions/workflows/index.yml"><img src="https://github.com/kite-plus/apps/actions/workflows/index.yml/badge.svg" alt="Index"></a>
  <a href="https://github.com/kite-plus/apps/actions/workflows/check.yml"><img src="https://github.com/kite-plus/apps/actions/workflows/check.yml/badge.svg" alt="Check"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-blue" alt="Apache License 2.0"></a>
</p>

<p align="center">
  <a href="README.md">English</a> · 简体中文
</p>

这个仓库是 [Kite](https://github.com/kite-plus/kite) 应用中心背后的索引：每个主题和插件一份简短的条目，加上由这些条目和各个包的 GitHub Release 生成的 [`index.json`](index.json)。Kite 从这里读取索引：

```
https://cdn.jsdelivr.net/gh/kite-plus/apps@main/index.json
```

jsDelivr 没有响应时，改读 `https://raw.githubusercontent.com/kite-plus/apps/main/index.json`。索引的格式见 [docs/index-format.md](docs/index-format.md)。

从 Kite 0.1.5 开始，站点可以按名字从这里安装：`kite theme add vane`、`kite plugin add search`，或者后台的「系统 → 应用中心」。

## 一个版本怎样被收录

每小时一次，以及条目有改动时，工作流会读取每个已收录仓库的 Release。标签为 `v<版本号>`、附带 `<id>-<版本号>.zip` 的 Release 会被下载，核对 GitHub 记录的大小和校验和，并按 Kite 安装一个包时的方式检查：

- 压缩包能安全解开，清单文件在最上层或唯一的那个文件夹里，并且只包含这个包用得到的文件；
- 清单里的 id 和版本号与条目、Release 一致，`apiVersion` 是 Kite 认识的，`requires` 能被某个已发布的 Kite 满足；
- 许可证允许索引分发它的副本，并且许可证全文随包一起；
- `kite theme verify` 或 `kite plugin verify` 通过，并报告这个包会让读者的浏览器从哪些其他站点加载资源、插件注入多少段代码、运行哪些钩子。

通过检查的版本会被复制进它自己的标签 `<kind>-<id>-<版本号>`，由 jsDelivr 分发，并连同校验和一起收录。GitHub 自己的下载地址作为第二个地址保留。

已收录的版本永远不会改变。替换一个已收录 Release 里的压缩包不会有任何效果，请发布新版本。

## 收录一个主题或插件

1. 把包放在公开的 GitHub 仓库里，在清单中用 SPDX 标识写明开源许可证，比如 `license: MIT`，并在清单旁放一份许可证全文 `LICENSE`。
2. 在包的目录里打包：

   ```sh
   kite theme pack    # 或：kite plugin pack
   ```

   它会写出 `dist/<id>-<版本号>.zip`，只包含站点用得到的文件，并打印大小和校验和。这个命令从 Kite 0.1.5 开始提供。
3. 发布一个标签为 `v<版本号>`（即清单里的版本号）的 GitHub Release，并附上这个压缩包。
4. 提一个 Pull Request，新增 `themes/<id>.yaml` 或 `plugins/<id>.yaml`：

   ```yaml
   kind: theme
   id: paper
   repo: someone/kite-theme-paper
   ```

   id 由小写单词和 `-` 组成，既是文件名，也是清单里的 id：主题的 `name`、插件的 `id`。`default` 是 Kite 内置主题的名字。

Pull Request 的检查会逐个版本列出索引将要收录的内容，以及每个版本从哪些站点加载资源。维护者阅读这些信息、查看这个包，然后合并。

## 新版本

新的 Release 会在一小时内被收录，不需要 Pull Request；除非它比最新的已收录版本要求更多：从那个版本没有用过的站点加载资源、运行那个版本没有运行的钩子，或者注入更多代码。这样的版本会等待，工作流会提一个 Pull Request，把它加进条目的 `approve`，并写明它多要了什么；如果组织不允许工作流提 Pull Request，就改开一个 issue，附上这处改动的链接。维护者合并它就会收录这个版本，关闭它则不收录。

```yaml
approve: [1.2.0]
```

## 撤回和下架

```yaml
yanked: [1.1.0]        # 不应再被安装的版本
delisted: Abandoned.   # 这个包不再被收录的原因
```

下架的包会在索引里保留它的版本，好让已经装了它的站点知道原因，但不会再收录新版本。直接删除条目则会让这个包悄无声息地从索引里消失。

只有 `kite-plus` 的包可以标为 `official: true`。

## 审核

合并一个条目之前，维护者确认：

- 检查通过，并且每个版本加载资源的站点都能由这个包的功能解释；
- 这个包做的就是它说的事：读一读它的模板，或者插件的源码；
- 提 Pull Request 的人维护这个包，或者得到了维护者的同意；
- id 不会和其他包的 id 混淆。

合并一个批准请求之前，维护者确认这个版本的发布说明讲清了它为什么要求更多，新加的站点也和说的一致。

## 自己运行

```sh
go run ./cmd/index -dry-run -kite "$(command -v kite)"
```

会检查每个新 Release 并打印将要收录的内容，不创建标签，也不写 `index.json`。`-only theme/vane` 只看一个条目；`-strict` 在某个条目的最新 Release 无法收录时失败，和 Pull Request 的检查一样。

## 许可证

Apache License 2.0。每个被收录的包保留它自己的许可证。
