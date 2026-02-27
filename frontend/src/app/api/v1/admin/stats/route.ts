import { NextResponse } from 'next/server';
import { requireAuth } from '@/lib/auth';
import { supabaseAdmin } from '@/lib/supabase';

export async function GET(request: Request) {
    try {
        const { auth, error: authError } = await requireAuth(request);
        if (authError) return authError;
        if (!auth || auth.role !== 'ADMIN') {
            return NextResponse.json({ status: 'error', message: 'Unauthorized' }, { status: 403 });
        }

        // 1. Total Users
        const { count: totalUsers, error: userError } = await supabaseAdmin
            .from('users')
            .select('*', { count: 'exact', head: true });

        // 2. Total Cows
        const { count: totalCows, error: cowError } = await supabaseAdmin
            .from('cows')
            .select('*', { count: 'exact', head: true });

        // 3. Aggregate Balances
        const { data: balances, error: balanceError } = await supabaseAdmin
            .from('users')
            .select('gold_balance, usdt_balance, points');

        if (userError || cowError || balanceError) throw new Error('Failed to fetch stats');

        let totalGold = 0;
        let totalUsdt = 0;
        let totalCowToken = 0;

        (balances as any[])?.forEach((u: any) => {
            totalGold += parseFloat(u.gold_balance) || 0;
            totalUsdt += parseFloat(u.usdt_balance) || 0;
            totalCowToken += parseFloat(u.points) || 0;
        });

        return NextResponse.json({
            status: 'success',
            data: {
                total_users: totalUsers || 0,
                total_cows: totalCows || 0,
                total_gold: totalGold.toFixed(2),
                total_usdt: totalUsdt.toFixed(2),
                total_cow_token: totalCowToken.toFixed(2)
            }
        }, { status: 200 });

    } catch (error: any) {
        console.error('Admin stats error:', error);
        return NextResponse.json(
            { status: 'error', message: 'Failed to fetch platform stats' },
            { status: 500 }
        );
    }
}
