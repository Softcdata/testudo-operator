#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_ROOT=$(dirname "${BASH_SOURCE[0]}")/..

# 从 vendor 获取 code-generator
CODEGEN_PKG=${CODEGEN_PKG:-$(cd "${SCRIPT_ROOT}"; ls -d -1 ./vendor/k8s.io/code-generator 2>/dev/null || echo ../code-generator)}

# 导入代码生成函数
source "${CODEGEN_PKG}/kube_codegen.sh"

# 设置模块和输出路径
MODULE="github.com/softcdata/testudo-operator"
APIS_PKG="pkg/apis"
OUTPUT_PKG="pkg"
GROUPS_VERSION="disaster:v1"

# 生成 deepcopy
kube::codegen::gen_helpers \
    --boilerplate "${SCRIPT_ROOT}/hack/boilerplate.go.txt" \
    "${SCRIPT_ROOT}/${APIS_PKG}"

# 生成客户端
kube::codegen::gen_client \
    --with-watch \
    --output-dir "${SCRIPT_ROOT}/${OUTPUT_PKG}" \
    --output-pkg "${MODULE}/${OUTPUT_PKG}" \
    --boilerplate "${SCRIPT_ROOT}/hack/boilerplate.go.txt" \
    "${SCRIPT_ROOT}/${APIS_PKG}"