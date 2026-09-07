-- Migration: 002_security_rls.sql
-- Purpose: Enable Row Level Security (RLS) on all tables according to Supabase Best Practices
-- Author: Antigravity Pair Programmer
-- Date: 2026-09-07

BEGIN;

-- ============================================================================
-- 1. ENABLE ROW LEVEL SECURITY ON ALL TABLES
-- ============================================================================
-- Protects tables against unauthorized direct PostgREST HTTP queries via anon key
ALTER TABLE users ENABLE ROW LEVEL SECURITY;
ALTER TABLE api_keys ENABLE ROW LEVEL SECURITY;
ALTER TABLE stock_prices ENABLE ROW LEVEL SECURITY;
ALTER TABLE stock_investments ENABLE ROW LEVEL SECURITY;
ALTER TABLE crypto_investments ENABLE ROW LEVEL SECURITY;
ALTER TABLE loans ENABLE ROW LEVEL SECURITY;

-- ============================================================================
-- 2. PUBLIC READ POLICIES (ANON & AUTHENTICATED)
-- ============================================================================
-- Stock prices are public market data and safe for public read
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_policies 
        WHERE tablename = 'stock_prices' AND policyname = 'allow_public_read_stock_prices'
    ) THEN
        CREATE POLICY allow_public_read_stock_prices 
            ON stock_prices FOR SELECT 
            TO anon, authenticated 
            USING (true);
    END IF;
END $$;

-- ============================================================================
-- 3. SERVICE ROLE POLICIES (FULL ACCESS FOR BACKEND)
-- ============================================================================
-- Ensures service_role and backend workers have full operational access
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE tablename = 'users' AND policyname = 'service_role_all_users') THEN
        CREATE POLICY service_role_all_users ON users FOR ALL TO service_role USING (true) WITH CHECK (true);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE tablename = 'api_keys' AND policyname = 'service_role_all_api_keys') THEN
        CREATE POLICY service_role_all_api_keys ON api_keys FOR ALL TO service_role USING (true) WITH CHECK (true);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE tablename = 'stock_investments' AND policyname = 'service_role_all_stock_investments') THEN
        CREATE POLICY service_role_all_stock_investments ON stock_investments FOR ALL TO service_role USING (true) WITH CHECK (true);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE tablename = 'stock_prices' AND policyname = 'service_role_all_stock_prices') THEN
        CREATE POLICY service_role_all_stock_prices ON stock_prices FOR ALL TO service_role USING (true) WITH CHECK (true);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE tablename = 'crypto_investments' AND policyname = 'service_role_all_crypto_investments') THEN
        CREATE POLICY service_role_all_crypto_investments ON crypto_investments FOR ALL TO service_role USING (true) WITH CHECK (true);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE tablename = 'loans' AND policyname = 'service_role_all_loans') THEN
        CREATE POLICY service_role_all_loans ON loans FOR ALL TO service_role USING (true) WITH CHECK (true);
    END IF;
END $$;

COMMIT;
