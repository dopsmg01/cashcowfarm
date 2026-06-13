import { NextResponse } from 'next/server';
import { requireAuth } from '@/lib/auth';
import { supabaseAdmin } from '@/lib/supabase';

export async function POST(request: Request) {
    try {
        const { auth, error: authError } = await requireAuth(request);
        if (authError) return authError;
        if (!auth) throw new Error('Auth context missing');

        const { listing_id } = await request.json();

        if (!listing_id) {
            return NextResponse.json({ status: 'error', message: 'Listing ID is required' }, { status: 400 });
        }

        // Fetch Buyer details
        const { data: buyer, error: buyerError } = await supabaseAdmin
            .from('users')
            .select('usdt_balance')
            .eq('id', auth.userId)
            .single();

        if (buyerError) throw buyerError;

        // Fetch Listing
        const { data: listing, error: listError } = await supabaseAdmin
            .from('market_listings')
            .select('*')
            .eq('id', listing_id)
            .eq('status', 'OPEN')
            .single();

        if (listError || !listing) {
            return NextResponse.json({ status: 'error', message: 'Listing tidak ditemukan atau sudah ditutup' }, { status: 422 });
        }

        if (buyer.usdt_balance < listing.price_usdt) {
            return NextResponse.json({ status: 'error', message: 'Saldo USDT tidak mencukupi' }, { status: 422 });
        }

        if (listing.seller_id === auth.userId) {
            return NextResponse.json({ status: 'error', message: 'Anda tidak dapat membeli item sendiri' }, { status: 422 });
        }

        // Execute Purchase Logic (Sequential pseudo-transaction)

        // 1. Deduct funds from buyer
        await supabaseAdmin
            .from('users')
            .update({ usdt_balance: buyer.usdt_balance - listing.price_usdt })
            .eq('id', auth.userId);

        // 2. Add funds to seller (Need to fetch seller first to update balance safely in a non-RPC way)
        const { data: seller } = await supabaseAdmin
            .from('users')
            .select('usdt_balance')
            .eq('id', listing.seller_id)
            .single();

        if (seller) {
            await supabaseAdmin
                .from('users')
                .update({ usdt_balance: seller.usdt_balance + listing.price_usdt })
                .eq('id', listing.seller_id);
        }

        // 3. Mark listing as SOLD
        await supabaseAdmin
            .from('market_listings')
            .update({ status: 'SOLD' })
            .eq('id', listing.id);

        // 4. Transfer Item (e.g. Grass) -> Simplistic MVP logic: add to inventory
        if (listing.item_type === 'GRASS' || listing.item_type === 'MILK') {
            const field = listing.item_type.toLowerCase();
            const { data: bInv } = await supabaseAdmin
                .from('inventories')
                .select(field)
                .eq('user_id', auth.userId)
                .single();

            if (bInv) {
                await supabaseAdmin
                    .from('inventories')
                    .update({ [field]: bInv[field] + listing.quantity })
                    .eq('user_id', auth.userId);
            }
        }

        // Create TxLogs (Optional for MVP, skipped here for brevity)

        return NextResponse.json({ status: 'success', message: 'Pembelian berhasil!' }, { status: 200 });

    } catch (error: any) {
        console.error('Buy error:', error);
        return NextResponse.json(
            { status: 'error', message: 'Internal server error' },
            { status: 500 }
        );
    }
}
