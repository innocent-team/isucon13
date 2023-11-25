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

if [[ "$INSTANCE_NUM" == 3 ]]; then
  sudo install -o root -g root -m 644 ./conf/etc/nginx/sites-enabled/isupipe.conf /etc/nginx/sites-enabled/isupipe.conf
  sudo install -o root -g root -m 644 ./conf/etc/nginx/nginx.conf /etc/nginx/nginx.conf
  sudo nginx -t

  sudo systemctl restart nginx
  sudo systemctl enable nginx
else
  sudo systemctl disable --now nginx.service
fi

# PDNS
# pdnsutil
sudo install -o root -g root -m 644 ./conf/etc/powerdns/pdns.d/gmysql-host.conf /etc/powerdns/pdns.d/gmysql-host.conf

# dist
if [[ "$INSTANCE_NUM" == 3 ]]; then
  sudo install -o root -g pdns -m 644 ./conf/etc/powerdns/pdns.conf /etc/powerdns/pdns.conf
  sudo systemctl enable --now pdns
else
  sudo systemctl disable --now pdns
fi
 
# pdns
if [[ "$INSTANCE_NUM" == 3 ]]; then
  sudo install -o root -g _dnsdist -m 640 ./conf/etc/dnsdist/dnsdist.conf /etc/dnsdist/dnsdist.conf
  sudo systemctl restart dnsdist
  sudo systemctl enable dnsdist
else
  sudo systemctl disable --now dnsdist
fi

# APP
if [[ "$INSTANCE_NUM" == 1 ||  "$INSTANCE_NUM" == 3 ]]; then
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
if [[ "$INSTANCE_NUM" == 3 || "$INSTANCE_NUM" == 2 ]]; then
  sudo install -o root -g root -m 644 ./conf/etc/mysql/mysql.conf.d/mysqld.cnf /etc/mysql/mysql.conf.d/mysqld.cnf

  echo "MySQL restart したいなら手動で sudo systemctl restart mysql やってね"
#  sudo systemctl restart mysql
  sudo systemctl enable --now mysql
else
  sudo systemctl disable --now mysql.service
fi
