# 规范：Operator 开发标准

## 概览
本规范定义了 `disaster-operator` 项目中开发 Kubernetes Operator 的标准工作流和最佳实践。

## ADDED Requirements

### Requirement: CRD 创建与代码生成
所有新的自定义资源定义 (CRD) 必须 (MUST) 遵循标准化的创建和生成流程。

#### Scenario: 创建新的 CRD
- **Given** 开发者需要引入一个新的 API 资源
- **When** 使用 `kubebuilder create api` 命令生成脚手架（默认沿用当前 GroupVersion）
- **And** 将生成的 API 文件移入 `pkg/apis/<group>/<version>/` 目录中
- **And** 在 `pkg/apis/<group>/<version>/xx_types.go` 中定义 Spec 和 Status 结构体
- **And** 执行 `make generate` 生成 DeepCopy 方法
- **And** 执行 `make manifests` 生成 CRD YAML 文件
- **And** 执行 `hack/update-codegen.sh` 生成 Clientset, Listers, 和 Informers
- **Then** 项目中应包含完整的 API 定义和生成的客户端代码

### Requirement: 控制器实现
控制器逻辑必须 (MUST) 健壮、可靠且易于监控。

#### Scenario: 编写 Reconcile 逻辑
- **Given** 一个新的控制器被创建
- **When** 实现 `Reconcile` 方法
- **Then** 该方法必须是**幂等**的（多次执行产生相同结果）
- **And** 必须包含结构化日志记录（使用 `logr`）
- **And** 应当记录关键指标（Metrics）以确保**可观测性**
- **And** 应当正确处理 Kubernetes 事件（Events）
