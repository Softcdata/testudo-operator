## MODIFIED Requirements

### Requirement: Failover 步骤执行
DisasterOperation 必须 (MUST) 按顺序执行 Failover 步骤，并在 PreCheck 失败时严格失败关闭。

#### Scenario: PreCheck dry-run 失败必须前置终止
- **GIVEN** Failover 开始
- **AND** modifier compile dry-run 失败
- **WHEN** PreCheck 结束
- **THEN** 操作必须 (MUST) 以失败状态终止
- **AND** 不得 (MUST NOT) 进入 `ScaleDownSource`
- **AND** 不得 (MUST NOT) 执行任何后续破坏性步骤

#### Scenario: PreCheck 失败不得出现 Completed
- **GIVEN** Failover 开始
- **AND** PreCheck 中任一 fail-closed 检查失败
- **WHEN** 操作结束
- **THEN** 终态不得 (MUST NOT) 为 `Completed`
- **AND** 必须 (MUST) 记录可定位错误码与失败步骤

#### Scenario: force 不得绕过 dry-run 失败
- **GIVEN** Failover 开始且 `force=true`
- **AND** modifier compile dry-run 失败
- **WHEN** PreCheck 结束
- **THEN** 操作必须 (MUST) 终止
- **AND** 不得 (MUST NOT) 进入 `ScaleDownSource`

## ADDED Requirements

### Requirement: Reprotect PreCheck 必须严格失败关闭

DisasterOperation 必须 (MUST) 在 Reprotect 进入破坏性阶段前执行 modifier compile dry-run，失败时不得继续执行。

#### Scenario: Reprotect dry-run 失败前置终止
- **GIVEN** Reprotect 开始
- **AND** modifier compile dry-run 失败
- **WHEN** PreCheck 结束
- **THEN** 必须 (MUST) 在破坏性步骤前终止操作
- **AND** 必须 (MUST) 记录失败步骤为 `PreCheck`
- **AND** 不得 (MUST NOT) 进入后续切换/缩容步骤
