#!/bin/sh


# Install sqlite3 database into /var/pdns/pdns.sqlite3

PDNS_DB_DIR=/var/pdns
PDNS_DB_NAME=pdns.sqlite3

if [ ! -f "$PDNS_DB_DIR/$PDNS_DB_NAME" ]
then
    curl https://raw.githubusercontent.com/PowerDNS/pdns/master/modules/gsqlite3backend/schema.sqlite3.sql | sqlite3 "$PDNS_DB_DIR/$PDNS_DB_NAME"
    chown -R pdns:pdns "$PDNS_DB_DIR"
    chmod -R u+rw,g+rw,o-rwx "$PDNS_DB_DIR"
fi


if [ "`ls -1 /root/.acme.sh/ | wc -l`" = "0" ]
then
    cd /src/acme.sh
    sh ./acme.sh --install
    cd -
fi


cat << EOF > /etc/resolve.conf
nameserver 127.0.0.1
nameserver 8.8.8.8
nameserver 8.8.4.4
EOF


# Start circus

circusd --log-level=ERROR /etc/circus/circus.ini
