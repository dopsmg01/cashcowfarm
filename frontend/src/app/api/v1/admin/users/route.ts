import { NextResponse } from 'next/server';
import { requireAuth } from '@/lib/auth';
import { supabaseAdmin } from '@/lib/supabase';

export async function GET(request: Request) {
    try {
        const { auth, error: authError } = await requireAuth(request);
        if (authError) return authError;
        if (!auth || auth.role !== 'ADMIN') {
            return NextResponse.json({ status: 'error', message: 'Unauthorized: Admin role required' }, { status: 403 });
        }

        // Fetch all users
        // Since Supabase join for count can be tricky in a single simple query without RPC or complex select,
        // we'll fetch users and then transform.
        const { data: users, error: userError } = await supabaseAdmin
            .from('users')
            .select(`
                id,
                wallet_address,
                role,
                gold_balance,
                usdt_balance,
                points,
                created_at
            `)
            .order('created_at', { ascending: false });

        if (userError) throw userError;

        // Fetch cow counts for all users to enrich the list
        const { data: cows, error: cowError } = await supabaseAdmin
            .from('cows')
            .select('owner_id');

        if (cowError) throw cowError;

        // Map cow counts
        const cowMap: Record<string, number> = {};
        cows.forEach(c => {
            cowMap[c.owner_id] = (cowMap[c.owner_id] || 0) + 1;
        });

        const enrichedUsers = users.map((u: any) => ({
            ...u,
            cow_count: cowMap[u.id] || 0,
            cow_token: u.points // mapping backend 'points' to frontend 'cow_token'
        }));

        return NextResponse.json({
            status: 'success',
            data: enrichedUsers
        }, { status: 200 });

    } catch (error: any) {
        console.error('Admin users error:', error);
        return NextResponse.json(
            { status: 'error', message: 'Failed to fetch admin users' },
            { status: 500 }
        );
    }
}
