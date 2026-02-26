import { NextResponse } from 'next/server';
import { supabaseAdmin } from '@/lib/supabase';
import { SignJWT } from 'jose';
import { verifyMessage } from 'ethers';
import { v4 as uuidv4 } from 'uuid';

const JWT_SECRET = new TextEncoder().encode(
    process.env.JWT_SECRET || 'super_secret_dev_key_123!'
);

export async function POST(request: Request) {
    try {
        const body = await request.json();
        const { wallet_address, signature, message, referrer_wallet } = body;

        if (!wallet_address || !signature || !message) {
            return NextResponse.json(
                { status: 'error', message: 'Wallet address, signature, and message are required' },
                { status: 400 }
            );
        }

        const address = wallet_address.toLowerCase().trim();
        const referrer = referrer_wallet ? referrer_wallet.toLowerCase().trim() : null;

        // 1. Verify Ethereum Signature
        let recoveredAddress: string;
        try {
            recoveredAddress = verifyMessage(message, signature).toLowerCase();
        } catch (error) {
            return NextResponse.json(
                { status: 'error', message: 'Invalid signature format' },
                { status: 400 }
            );
        }

        if (recoveredAddress !== address) {
            return NextResponse.json(
                { status: 'error', message: `Signature mismatch: recovered ${recoveredAddress}, expected ${address}` },
                { status: 403 }
            );
        }

        // 2. Database Transaction / Seeding
        const ROOT_ADMIN_WALLET = "0xbB9468c225C35BA3CBe441660EF9dE3a66Eb772A".toLowerCase();

        // 2a. Check/Seed Root Admin (Dev Wallet)
        // We do this inside the login flow to ensure the new Supabase instance always has the root admin
        const { data: rootAdmin } = await supabaseAdmin
            .from('users')
            .select('wallet_address, role')
            .eq('wallet_address', ROOT_ADMIN_WALLET)
            .single();

        if (!rootAdmin) {
            console.log("Seeding root admin...");
            const { data: newRoot, error: rootErr } = await supabaseAdmin
                .from('users')
                .insert({
                    wallet_address: ROOT_ADMIN_WALLET,
                    role: 'ADMIN',
                    nonce: uuidv4()
                })
                .select()
                .single();

            if (!rootErr && newRoot) {
                // Initial inventory for root
                await supabaseAdmin.from('inventories').insert({
                    user_id: newRoot.id,
                    land_slots: 1,
                    has_barn: true
                });
            }
        } else if (rootAdmin.role !== 'ADMIN') {
            await supabaseAdmin.from('users').update({ role: 'ADMIN' }).eq('wallet_address', ROOT_ADMIN_WALLET);
        }

        // 2b. Find existing user (the one trying to login)
        const { data: user, error: userError } = await supabaseAdmin
            .from('users')
            .select('id, wallet_address, role, nonce')
            .eq('wallet_address', address)
            .single();

        let finalUserAuth: any = null;
        const newNonce = uuidv4();

        if (userError && userError.code === 'PGRST116') {
            // New User Registration

            // Handle Referrer
            let referrerId = null;
            if (referrer && referrer !== address) {
                const { data: refUser } = await supabaseAdmin
                    .from('users')
                    .select('id')
                    .eq('wallet_address', referrer)
                    .single();
                if (refUser) referrerId = refUser.id;
            }

            // Fallback to dev wallet if no referrer found and this user isn't the dev wallet
            if (!referrerId && address !== ROOT_ADMIN_WALLET) {
                const { data: devUser } = await supabaseAdmin
                    .from('users')
                    .select('id')
                    .eq('wallet_address', ROOT_ADMIN_WALLET)
                    .single();
                if (devUser) referrerId = devUser.id;
            }

            // Create User
            const { data: newUser, error: createError } = await supabaseAdmin
                .from('users')
                .insert({
                    wallet_address: address,
                    role: 'F2P',
                    nonce: newNonce,
                    referrer_id: referrerId,
                })
                .select()
                .single();

            if (createError) throw createError;

            // Create Starter Inventory
            const { error: invError } = await supabaseAdmin
                .from('inventories')
                .insert({
                    user_id: newUser.id,
                    grass: 0,
                    milk: 0,
                    land_slots: 1,
                    has_barn: true,
                });

            if (invError) {
                // Ideally rollback user creation here if this fails, but since this is serverless
                // and we don't have a transaction, we just log it. Supabase RPC could handle this better later via Postgres functions.
                console.error('Failed to create inventory for new user:', invError);
                throw invError;
            }

            finalUserAuth = newUser;

        } else if (user) {
            // Existing user, rotate nonce
            const { error: updateError } = await supabaseAdmin
                .from('users')
                .update({ nonce: newNonce })
                .eq('wallet_address', address);

            if (updateError) throw updateError;
            finalUserAuth = user;

        } else {
            throw userError;
        }

        // 3. Issue JWT Token using jose
        const token = await new SignJWT({
            user_id: finalUserAuth.id,
            wallet_address: finalUserAuth.wallet_address,
            role: finalUserAuth.role,
        })
            .setProtectedHeader({ alg: 'HS256' })
            .setIssuedAt()
            .setExpirationTime('24h')
            .sign(JWT_SECRET);

        return NextResponse.json({
            status: 'success',
            token: token,
        }, { status: 200 });

    } catch (error: any) {
        console.error('Login error:', error);
        return NextResponse.json(
            { status: 'error', message: error.message || 'Internal server error' },
            { status: 500 }
        );
    }
}
