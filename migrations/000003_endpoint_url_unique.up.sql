-- Dois endpoints com a mesma URL na mesma application receberiam entregas
-- idênticas em duplicata. O índice é regra de domínio, e de quebra faz o
-- clique duplo do dashboard virar 409 em vez de lixo com secret novo.
--
-- Sem CONCURRENTLY porque a tabela está vazia; sobre tabela em uso ele seria
-- obrigatório para não travar escrita.
CREATE UNIQUE INDEX endpoints_application_url_idx
    ON endpoints (application_id, url);
