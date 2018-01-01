FROM alpine:3.5

RUN apk update && \
    apk add --no-cache openssl socat curl && \
    apk add --no-cache vim nginx pdns pdns-backend-sqlite3 python3 && \
    python3 -m ensurepip && \
    rm -rf /usr/lib/python*/ensurepip && \
    pip3 install --upgrade pip setuptools && \
    if [ ! -e /usr/bin/pip ]; then ln -s /usr/bin/pip3 /usr/bin/pip ; fi && \
    if [ ! -e /usr/bin/python ]; then ln -s /usr/bin/python3 /usr/bin/python ; fi && \
    rm -rf /root/.cache

RUN apk add --no-cache --virtual .deps \
        build-base linux-headers \
        libc-dev libstdc++ libgcc \
        gcc g++ make git \
        python3-dev && \
    pip install circus jinja2 && \
    git clone https://github.com/Neilpang/acme.sh.git /src/acme.sh && \
    cd /src/acme.sh && ./acme.sh --install && cd - \
    rm -rf /src/acme.sh && \
    apk del .deps

RUN mkdir /run/nginx && \
    mkdir -p /var/www && \
    mkdir -p /etc/sslcerts && \
    chown nginx:nginx /run/nginx /var/www /etc/sslcerts && \
    mkdir /var/pdns /etc/pdns/pdns.d && \
    rm -rf /etc/nginx/conf.d/*

COPY ./confs/powerdns/pdns.conf /etc/pdns/pdns.conf
COPY ./confs/circus/ /etc/circus/
COPY ./scripts/ /scripts/

RUN chmod u+x /scripts/*
ENV PATH=/scripts:$PATH

VOLUME /var/pdns
VOLUME /etc/sslcerts
VOLUME /root/.acme.sh
VOLUME /etc/nginx/conf.d

EXPOSE 53
EXPOSE 80
EXPOSE 443

CMD ["start.sh"]
