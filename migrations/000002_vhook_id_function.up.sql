-- Decodifica a forma externa de um identificador de volta para o uuid
-- armazenado, para que uma investigação escreva direto no psql:
--
--   SELECT * FROM events WHERE id = vhook_id('evt_01HX62MYSHFH79MARZBJ6KWTR4');
--
-- Existe porque o psql mostra o uuid cru, e a API mostra base32 (§4.31).
-- Um teste de integração roda os mesmos vetores contra esta função e contra
-- a implementação Go, porque duas cópias do mesmo encoding divergem sozinhas.
CREATE OR REPLACE FUNCTION vhook_id(external text)
RETURNS uuid
LANGUAGE plpgsql
IMMUTABLE
STRICT
AS $$
DECLARE
    alphabet CONSTANT text := '0123456789ABCDEFGHJKMNPQRSTVWXYZ';
    hexdigits CONSTANT text := '0123456789abcdef';
    max128 CONSTANT numeric := 340282366920938463463374607431768211455;
    body text;
    acc  numeric := 0;
    pos  integer;
    hex  text := '';
BEGIN
    body := upper(external);

    -- Prefixo de recurso é opcional e não é validado aqui: quem valida que
    -- um evt_ não virou dlv_ é a camada de aplicação, com erro nomeado.
    IF body ~ '^[A-Z]{3}_' THEN
        body := substring(body from 5);
    END IF;

    -- Ambiguidades do alfabeto Crockford.
    body := translate(body, 'ILO', '110');

    IF body !~ '^[0-9A-Z]{26}$' THEN
        RAISE EXCEPTION 'invalid vhook id: %', external
            USING ERRCODE = 'invalid_text_representation';
    END IF;

    FOR i IN 1..26 LOOP
        pos := position(substring(body from i for 1) in alphabet);
        IF pos = 0 THEN
            RAISE EXCEPTION 'invalid vhook id: %', external
                USING ERRCODE = 'invalid_text_representation';
        END IF;
        acc := acc * 32 + (pos - 1);
    END LOOP;

    -- 26 caracteres carregam 130 bits; só 128 são válidos.
    IF acc > max128 THEN
        RAISE EXCEPTION 'invalid vhook id: %', external
            USING ERRCODE = 'invalid_text_representation';
    END IF;

    FOR i IN 1..32 LOOP
        hex := substring(hexdigits from (mod(acc, 16)::integer + 1) for 1) || hex;
        acc := div(acc, 16);
    END LOOP;

    RETURN hex::uuid;
END;
$$;
