#! /bin/bash

cd `dirname $0`

echo "Current Host: $HOSTNAME"

if [[ "$HOSTNAME" == isucon1 ]]; then
  INSTANCE_NUM="1"
elif [[ "$HOSTNAME" == isucon2 ]]; then
  INSTANCE_NUM="2"
elif [[ "$HOSTNAME" == isucon3 ]]; then
  INSTANCE_NUM="3"
else
  echo "Invalid host"
  exit 1
fi

set -ex

git pull

sudo systemctl daemon-reload

# env
install -o isucon -g isucon -m 755 ./conf/env/${HOSTNAME}/env.sh /home/isucon/env.sh

# NGINX
#
#if [[ "$INSTANCE_NUM" == 1 ]]; then
#  sudo install -o root -g root -m 644 ./conf/etc/nginx/sites-available/isuconquest.conf /etc/nginx/sites-available/isuconquest.conf
#  sudo install -o root -g root -m 644 ./conf/etc/nginx/nginx.conf /etc/nginx/nginx.conf
#  sudo nginx -t
#
#  sudo systemctl restart nginx
#  sudo systemctl enable nginx
#fi

#if [[ "$INSTANCE_NUM" != 1 ]]; then
#  sudo systemctl stop nginx.service
#  sudo systemctl disable nginx.service
#fi

# APP
if [[ "$INSTANCE_NUM" == 1 || "$INSTANCE_NUM" == 2 || "$INSTANCE_NUM" == 3 ]]; then
  sudo install -o root -g root -m 644 ./conf/etc/systemd/system/isupipe-go.service /etc/systemd/system/isupipe-go.service
  sudo systemctl daemon-reload

  pushd go
  make build
  popd
  sudo systemctl restart isupipe-go.service
  sudo systemctl enable --now isupipe-go.service
  
  sleep 2
  
  sudo systemctl status isupipe-go.service --no-pager
else
  sudo systemctl disable --now isupipe-go.service
fi

# MYSQL
if [[ "$INSTANCE_NUM" == 1 || "$INSTANCE_NUM" == 2 || "$INSTANCE_NUM" == 3 ]]; then
  sudo install -o root -g root -m 644 ./conf/etc/mysql/mysql.conf.d/mysqld.cnf /etc/mysql/mysql.conf.d/mysqld.cnf

  echo "MySQL restart したいなら手動で sudo systemctl restart mysql やってね"
#  sudo systemctl restart mysql
  sudo systemctl enable --now mysql
else
  echo TODO PDNS
  sudo systemctl disable --now mysql.service
fi
