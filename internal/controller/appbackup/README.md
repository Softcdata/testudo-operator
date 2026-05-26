appbackup_controller.go：主要控制器文件，实现应用备份的业务逻辑，包括资源监听、事件处理和备份操作。
appbackup_controller_test.go：控制器单元测试文件，验证控制器逻辑的正确性。
appbackup_pending.go：处理应用备份待处理状态的具体实现，可能包括等待条件或准备工作。
appbackup_ready.go：处理应用备份就绪状态的逻辑，确保备份可以开始执行。
appbackup_state.go：管理应用备份的状态机或状态相关逻辑，确保备份过程的可靠性和一致性。
appbackup_state_test.go：状态管理单元测试文件，验证状态逻辑的正确性。
suite_test.go：测试套件文件，用于设置和运行集成测试或端到端测试。