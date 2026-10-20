FROM python:3.12-slim

# Stdout/stderr sem buffer — logs aparecem na hora em `docker logs` (essencial
# no modo --daemon, que fica a maior parte do tempo dormindo até o horário
# diário configurado).
ENV PYTHONUNBUFFERED=1

WORKDIR /app

RUN pip install --no-cache-dir \
    pyyaml \
    requests \
    oracledb

COPY bridge_fornecedores.py .

# config.yaml é montado em /app/config.yaml na hora de rodar (bind mount) —
# nunca baked na imagem, contém a API key. Sem tracker/estado local: cada
# execução é um resync completo e idempotente, não precisa de volume.

ENTRYPOINT ["python", "bridge_fornecedores.py"]
CMD ["--daemon"]
