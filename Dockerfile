FROM alpine:3.5


RUN apk update && \
    apk add build-base linux-headers libc-dev libstdc++ libgcc gcc g++ make && \
    apk add python3-dev

RUN apk add vim nginx dnsmasq openssl socat && \
    apk add --no-cache python3


RUN python3 -m ensurepip && \
    rm -r /usr/lib/python*/ensurepip && \
    pip3 install --upgrade pip setuptools && \
    if [ ! -e /usr/bin/pip ]; then ln -s /usr/bin/pip3 /usr/bin/pip ; fi && \
    if [ ! -e /usr/bin/python ]; then ln -s /usr/bin/python3 /usr/bin/python ; fi && \
    rm -rf /root/.cache

RUN pip install circus jinja2

RUN apk add curl && curl https://get.acme.sh | sh


RUN mkdir /run/nginx && \
    mkdir -p /var/www && \
    mkdir -p /etc/sslcerts && \
    chown nginx:nginx /run/nginx /var/www /etc/sslcerts


COPY ./confs/nginx/vhosts/ /etc/nginx/conf.d/
COPY ./confs/circus/ /etc/circus/


COPY ./scripts/ /scripts/
RUN chmod u+x /scripts/* && \
    echo 'export PATH=/scripts:$PATH' >> /etc/profile && \
    echo '\
        . /etc/profile ; \
    ' >> /root/.profile


CMD ["circusd", "--log-level=ERROR", "/etc/circus/circus.ini"]
