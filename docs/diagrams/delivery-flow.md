# Vida de uma entrega

Do ingress até a DLQ, com uma falha no meio. Decisões relacionadas: [`ARCHITECTURE.md`](../ARCHITECTURE.md) §4.2 (escada de retry), §4.4 (ordem de ack), §4.6 (mensagem magra).

```mermaid
sequenceDiagram
    autonumber
    participant P as produtor
    participant A as api
    participant DB as Postgres
    participant Q as RabbitMQ
    participant W as worker
    participant C as endpoint do cliente

    P->>A: POST /v1/events
    A->>DB: grava event + uma delivery por endpoint
    A->>Q: publica {delivery_id, attempt:1}
    Q-->>A: publisher confirm
    A-->>P: 202 Accepted

    Note over A,P: resposta imediata, zero processamento pesado

    Q->>W: entrega a mensagem
    W->>DB: busca payload, url e secret
    Note over W,DB: mensagem magra: o worker vê o estado ATUAL,<br/>então endpoint desativado não recebe

    W->>C: POST assinado, timeout 5s
    C-->>W: 503
    W->>DB: grava attempt 1 (status_code 503)
    W->>Q: publica na wait do nível 1
    Q-->>W: confirm
    W->>Q: ack da mensagem original

    Note over W,Q: ack SÓ depois do confirm.<br/>Invertido, uma morte do worker perderia o evento.

    Note over Q: mensagem dorme na wait até o TTL vencer
    Q->>Q: dead-letter de volta para a exchange principal

    Q->>W: entrega novamente {attempt:2}
    W->>C: POST assinado
    C-->>W: 200
    W->>DB: grava attempt 2, delivery = succeeded
    W->>Q: ack
```

## Quando as tentativas esgotam

```mermaid
flowchart LR
    F["falha na última<br/>tentativa"] --> P["worker publica<br/>na exchange de DLQ"]
    P --> D["fila dlq"]
    P --> S["delivery.status = dead"]
    S --> R["botão de replay<br/>no dashboard"]
    R --> M["volta para a fila principal<br/>com attempt reiniciado"]

    F -.->|"NUNCA por nack"| X["dead-letter<br/>na fila deliveries"]

    style X stroke-dasharray: 5 5
```

O caminho pontilhado é o erro que parece natural: configurar `x-dead-letter-exchange` na fila `deliveries`. Ele colidiria com a escada de espera e mandaria a mensagem para a DLQ **na primeira falha**, não na última — sem lançar erro nenhum. Ver §4.3.

## O que não aparece aqui

**Falha permanente não passa pela escada.** Um 4xx (exceto 408 e 429) vai direto para `failed`, sem retry: o cliente está rejeitando de propósito, e insistir só queima worker.

**O reconciliador é o caminho de exceção.** Se o publish falhar ou o worker morrer deixando `delivering` pendurado, a linha em `deliveries` continua no Postgres e é republicada. Ver §4.20.
