# Introduce Cancel Operation

## Metadata
- **Status**: Proposed
- **Created**: 2026-02-06
- **Authors**: Antigravity
- **Type**: Feature

## Summary
引入 `Cancel` 操作，允许用户中止并回滚处于 `FailingOver` 状态的 DisasterInstance。该操作将明确地将系统状态复原到 Failover 之前的 `Protected` 状态，消除当前 `Undo` 操作在中间状态下的歧义和逻辑缺陷。

## Motivation
目前 `Undo` 操作被重载用于两种场景：
1. `FailingOver` 状态下的回滚（Cancel）。
2. `Active` 状态下的撤销（Reverse to Protected）。

在 `FailingOver` 状态下（Failover 未完成，角色未切换），当前的实现复用了依赖状态的 ScaleUp/ScaleDown 逻辑，导致操作目标错误（试图 ScaleDown Source / ScaleUp Target）。引入明确的 `Cancel` 操作可以针对性地处理这一场景，显式操作 Config 中定义的 Target/Source 集群，确保回滚的安全性。

## Goals
1. 支持 `Cancel` 操作类型 (`OperationTypeCancel`)。
2. 仅允许在 `FailingOver` 状态下执行 `Cancel`。
3. `Cancel` 操作执行后，Instance 状态应变回 `Protected`。
4. 修复回滚逻辑：显式缩容 Target 集群，扩容 Source 集群，恢复同步。

## Non-Goals
- 修改 `Active` 状态下的 `Undo` 行为（暂时保留，或后续重命名为 Failback-Undo）。
