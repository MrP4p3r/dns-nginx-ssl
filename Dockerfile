FROM alpine:3.5


RUN apk update && \
    apk add --no-cache --virtual .deps \
        build-base linux-headers \
        libc-dev libstdc++ libgcc \
        gcc g++ make git \
        python3-dev && \
    apk add --no-cache openssl socat curl && \
    apk add --no-cache \
        vim nginx pdns pdns-backend-sqlite3 python3


RUN python3 -m ensurepip && \
    rm -r /usr/lib/python*/ensurepip && \
    pip3 install --upgrade pip setuptools && \
    if [ ! -e /usr/bin/pip ]; then ln -s /usr/bin/pip3 /usr/bin/pip ; fi && \
    if [ ! -e /usr/bin/python ]; then ln -s /usr/bin/python3 /usr/bin/python ; fi && \
    rm -rf /root/.cache

RUN pip install circus jinja2

RUN git clone https://github.com/Neilpang/acme.sh.git /src/acme.sh && \
    mkdir /run/nginx && \
    mkdir -p /var/www && \
    mkdir -p /etc/sslcerts && \
    chown nginx:nginx /run/nginx /var/www /etc/sslcerts


COPY ./confs/powerdns/pdns.conf /etc/pdns/pdns.conf
COPY ./confs/circus/ /etc/circus/
COPY ./confs/resolv.conf /etc/resolv.conf.bak

RUN mkdir /var/pdns /etc/pdns/pdns.d && \
    rm -rf /etc/nginx/conf.d/*


COPY ./scripts/ /scripts/
RUN chmod u+x /scripts/* && \
    echo 'export PATH=/scripts:$PATH' >> /etc/profile && \
    echo '\
        . /etc/profile ; \
    ' >> /root/.profile

RUN apk del .deps

EXPOSE 53
EXPOSE 80
EXPOSE 443

CMD ["/scripts/start.sh"]
