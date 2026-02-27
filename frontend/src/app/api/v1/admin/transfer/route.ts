import { NextResponse } from 'next/server';
import { requireAuth } from '@/lib/auth';
import { supabaseAdmin } from '@/lib/supabase';

export async function POST(request: Request) {
    try {
        const { auth, error: authError } = await requireAuth(request);
        if (authError) return authError;
        if (!auth || auth.role !== 'ADMIN') {
            return NextResponse.json({ status: 'error', message: 'Unauthorized' }, { status: 403 });
        }

        const { target_wallet, item_type, amount } = await request.json();
        const address = target_wallet?.toLowerCase().trim();
        const amt = parseFloat(amount);

        if (!address || !item_type || isNaN(amt)) {
            return NextResponse.json({ status: 'error', message: 'Missing target_wallet, item_type, or invalid amount' }, { status: 400 });
        }

        // Logic: Add to the user's specific balance
        let column = '';
        if (item_type === 'GOLD') column = 'gold_balance';
        else if (item_type === 'USDT') column = 'usdt_balance';
        else if (item_type === 'COW') column = 'points';
        else return NextResponse.json({ status: 'error', message: 'Invalid item_type. Use GOLD, USDT, or COW' }, { status: 400 });

        // Update in Supabase
        // Note: This is an additive update. For safety, we first fetch then update, 
        // or use an increment RPC if we had it.
        const { data: user, error: fetchErr } = await supabaseAdmin
            .from('users')
            .select(`id, ${column}`)
            .eq('wallet_address', address)
            .single();

        const userData = user as any;
        const currentVal = parseFloat(userData[column]) || 0;
        const newVal = currentVal + amt;

        const { error: updateErr } = await supabaseAdmin
            .from('users')
            .update({ [column]: newVal })
            .eq('wallet_address', address);

        if (updateErr) throw updateErr;

        return NextResponse.json({
            status: 'success',
            message: `Successfully transferred ${amt} ${item_type} to ${address}`
        }, { status: 200 });

    } catch (error: any) {
        console.error('Admin transfer error:', error);
        return NextResponse.json(
            { status: 'error', message: 'Transfer failed' },
            { status: 500 }
        );
    }
}
