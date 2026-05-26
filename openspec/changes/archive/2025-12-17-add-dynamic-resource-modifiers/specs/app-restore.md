# AppRestore Specification Delta

## ADDED Requirements

#### Scenario: Dynamic Resource Modification
Given an AppRestore CR with `resourceModifierRules` defined
When the controller processes the AppRestore
Then it should create a ConfigMap in the Velero namespace containing the modifier rules
And the created Velero Restore should reference this ConfigMap
And the ConfigMap should be deleted when the AppRestore is deleted
