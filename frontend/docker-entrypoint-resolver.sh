#!/bin/sh
# nginx 官方镜像 /docker-entrypoint.d/ 钩子（20-envsubst 渲染后、nginx 启动前执行）：
# 从容器 resolv.conf 提取第一个 nameserver，把模板里的 #RESOLVER# 占位换成 resolver 指令。
# 为什么：proxy_pass 写死主机名时 nginx 启动期就做静态解析——本地 docker run 无 K8s DNS、
# 或集群里引擎 service 晚创建，都会让 nginx 直接 emerg 拒启。配了 resolver 后变为
# 运行时解析：解析不到只会 502，进程照常活着（kube-dns / docker embedded dns 通吃）。
set -e
NS=$(awk '/^nameserver/ { print $2; exit }' /etc/resolv.conf)
CONF=/etc/nginx/conf.d/default.conf
if [ -n "$NS" ] && grep -q '#RESOLVER#' "$CONF"; then
  sed -i "s|#RESOLVER#|resolver ${NS} valid=10s;|" "$CONF"
  echo "99-resolver: injected 'resolver ${NS} valid=10s;' into ${CONF}"
fi
