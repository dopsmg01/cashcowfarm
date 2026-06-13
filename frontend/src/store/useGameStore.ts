import { create } from 'zustand';
import { persist, createJSONStorage } from 'zustand/middleware';

interface CowData {
    id: string;
    level: number;
    happiness: number;
    lastFedAt: string | null;
}

interface InventoryData {
    grass: number;
    milk: number;
    landSlots: number;
}

interface AdminUser {
    id: string;
    wallet_address: string;
    role: string;
    gold_balance: string;
    usdt_balance: string;
    cow_token: string;
    cow_count: number;
    created_at: string;
}

interface PlatformStats {
    total_users: number;
    total_cows: number;
    total_gold: string;
    total_usdt: string;
    total_cow_token: string;
}

interface GameState {
    // Auth
    authToken: string | null;
    userRole: string | null;
    userWallet: string | null;
    isAuthenticated: boolean;
    isRegistered: boolean;
    needsRegistration: boolean;

    // Game Data
    usdtBalance: number;
    goldBalance: number;
    cowTokenBalance: number;
    grassCount: number;
    milkCount: number;
    landSlots: number;
    dailyAdCount: number;
    hasBarn: boolean;
    web2Stakes: any[];
    cows: CowData[];
    isLoggedIn: boolean;
    isLoading: boolean;

    // Admin
    adminUsers: AdminUser[];
    platformStats: PlatformStats | null;

    // Actions
    setBalance: (amt: number) => void;
    setGrass: (amt: number) => void;
    setMilk: (amt: number) => void;
    setCows: (cows: CowData[]) => void;
    setLoading: (loading: boolean) => void;
    login: () => void;
    logout: () => void;

    // Auth Actions
    authenticate: (address: string, signMessageFn: (args: { message: string }) => Promise<string>, referrer?: string) => Promise<boolean>;
    checkRegistration: (address: string) => Promise<{ exists: boolean; nonce: string }>;
    setNeedsRegistration: (needed: boolean) => void;
    clearAuth: () => void;

    // Game Actions
    fetchFarmData: () => Promise<void>;
    stakeInApp: (assetType: string, amount: number) => Promise<void>;
    claimInApp: () => Promise<void>;
    deposit: (asset: string, amount: number) => Promise<void>;
    withdraw: (asset: string, amount: number) => Promise<void>;

    // Admin Actions
    fetchAdminUsers: () => Promise<void>;
    fetchPlatformStats: () => Promise<void>;
    adminTransfer: (targetWallet: string, itemType: string, amount: string) => Promise<{ success: boolean; message: string }>;
}

const API_BASE = process.env.NEXT_PUBLIC_API_URL || '/api/v1';

// Dev wallet address for admin detection
const DEV_WALLET = '0xbB9468c225C35BA3CBe441660EF9dE3a66Eb772A'.toLowerCase();

export const useGameStore = create<GameState>()(
    persist(
        (set, get) => ({
            // Auth State
            authToken: null,
            userRole: null,
            userWallet: null,
            isAuthenticated: false,
            isRegistered: false,
            needsRegistration: false,

            // Game State
            usdtBalance: 0,
            goldBalance: 0,
            cowTokenBalance: 0,
            grassCount: 0,
            milkCount: 0,
            landSlots: 1,
            dailyAdCount: 0,
            hasBarn: false,
            web2Stakes: [],
            cows: [],
            isLoggedIn: false,
            isLoading: false,

            // Admin State
            adminUsers: [],
            platformStats: null,

            setBalance: (amt) => set({ usdtBalance: amt }),
            setGrass: (amt) => set({ grassCount: amt }),
            setMilk: (amt) => set({ milkCount: amt }),
            setCows: (cows) => set({ cows }),
            setLoading: (loading) => set({ isLoading: loading }),
            login: () => set({ isLoggedIn: true }),
            logout: () => {
                set({
                    isLoggedIn: false,
                    isAuthenticated: false,
                    isRegistered: false,
                    needsRegistration: false,
                    authToken: null,
                    userRole: null,
                    userWallet: null,
                    usdtBalance: 0,
                    goldBalance: 0,
                    cowTokenBalance: 0,
                    grassCount: 0,
                    milkCount: 0,
                    cows: [],
                    dailyAdCount: 0,
                    adminUsers: [],
                    platformStats: null,
                });
            },

            checkRegistration: async (address: string) => {
                try {
                    const res = await fetch(`${API_BASE}/auth/nonce/${address}`);
                    if (!res.ok) throw new Error('Failed to fetch nonce');
                    const data = await res.json();
                    set({ isRegistered: data.exists });
                    return { exists: data.exists, nonce: data.nonce };
                } catch (e) {
                    console.error('Check registration failed:', e);
                    return { exists: false, nonce: '' };
                }
            },

            setNeedsRegistration: (needed: boolean) => set({ needsRegistration: needed }),

            // Auth: Connect + Sign flow
            authenticate: async (address: string, signMessageFn: (args: { message: string }) => Promise<string>, referrer?: string) => {
                try {
                    set({ isLoading: true });

                    // 1. Get nonce from backend
                    const nonceRes = await fetch(`${API_BASE}/auth/nonce/${address}`);
                    if (!nonceRes.ok) throw new Error('Failed to get nonce');
                    const { nonce } = await nonceRes.json();

                    // 2. Construct the message to sign
                    const message = `Welcome to Cash Cow Valley!\n\nProving ownership of your wallet.\n\nWallet: ${address}\nNonce: ${nonce}`;

                    // 3. Request signature from wallet
                    const signature = await signMessageFn({ message });

                    // 4. Send to backend for verification
                    const loginRes = await fetch(`${API_BASE}/auth/login`, {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({
                            wallet_address: address,
                            signature,
                            message,
                            referrer_wallet: referrer || '',
                        }),
                    });

                    if (!loginRes.ok) {
                        const err = await loginRes.json();
                        throw new Error(err.message || 'Login failed');
                    }

                    const { token } = await loginRes.json();

                    // 5. Decode role from JWT
                    let role = 'F2P';
                    try {
                        const payload = JSON.parse(atob(token.split('.')[1]));
                        role = payload.role || 'F2P';
                    } catch (e) { /* fallback to F2P */ }

                    set({
                        authToken: token,
                        userRole: role,
                        userWallet: address.toLowerCase(),
                        isAuthenticated: true,
                        isLoggedIn: true,
                        isRegistered: true,
                        needsRegistration: false,
                    });

                    // 6. Fetch farm data after auth
                    await get().fetchFarmData();

                    return true;
                } catch (e) {
                    console.error('Authentication failed:', e);
                    return false;
                } finally {
                    set({ isLoading: false });
                }
            },

            clearAuth: () => {
                set({
                    authToken: null,
                    userRole: null,
                    userWallet: null,
                    isAuthenticated: false,
                    isLoggedIn: false,
                });
            },

            fetchFarmData: async () => {
                const token = get().authToken;
                if (!token) return;
                try {
                    set({ isLoading: true });
                    const res = await fetch(`${API_BASE}/farm/status`, {
                        headers: { 'Authorization': `Bearer ${token}` }
                    });
                    if (!res.ok) {
                        const errBody = await res.json().catch(() => ({}));
                        console.error('Failed to fetch farm data:', res.status, errBody.message || errBody.error);
                        return;
                    }
                    const json = await res.json();
                    const data = json.data;
                    if (data) {
                        set({
                            cows: data.cows || [],
                            grassCount: data.inventory?.grass || 0,
                            milkCount: data.inventory?.milk || 0,
                            landSlots: data.inventory?.land_slots || 1,
                            usdtBalance: parseFloat(data.usdt_balance) || 0,
                            goldBalance: parseFloat(data.gold_balance) || 0,
                            cowTokenBalance: parseFloat(data.points) || 0,
                            dailyAdCount: data.daily_ad_count || 0,
                            hasBarn: data.inventory?.has_barn || false,
                            web2Stakes: data.web2_stakes || [],
                        });
                    }
                } catch (e) {
                    console.error('Failed to fetch farm data:', e);
                } finally {
                    set({ isLoading: false });
                }
            },

            stakeInApp: async (assetType, amount) => {
                const token = get().authToken;
                if (!token) return;
                try {
                    const res = await fetch(`${API_BASE}/market/stake-inapp`, {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${token}` },
                        body: JSON.stringify({ asset_type: assetType, amount })
                    });
                    if (!res.ok) {
                        const errBody = await res.json().catch(() => ({}));
                        console.error('Stake failed:', errBody.message || res.statusText);
                        return;
                    }
                    await get().fetchFarmData();
                } catch (e) { console.error('Stake error:', e); }
            },

            claimInApp: async () => {
                const token = get().authToken;
                if (!token) return;
                try {
                    const res = await fetch(`${API_BASE}/market/claim-inapp`, {
                        method: 'POST',
                        headers: { 'Authorization': `Bearer ${token}` }
                    });
                    if (!res.ok) {
                        const errBody = await res.json().catch(() => ({}));
                        console.error('Claim failed:', errBody.message || res.statusText);
                        return;
                    }
                    await get().fetchFarmData();
                } catch (e) { console.error('Claim error:', e); }
            },

            deposit: async (asset, amount) => {
                const token = get().authToken;
                if (!token) return;
                try {
                    const res = await fetch(`${API_BASE}/market/deposit`, {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${token}` },
                        body: JSON.stringify({ asset, amount })
                    });
                    if (!res.ok) {
                        const errBody = await res.json().catch(() => ({}));
                        console.error('Deposit failed:', errBody.message || res.statusText);
                        return;
                    }
                    await get().fetchFarmData();
                } catch (e) { console.error('Deposit error:', e); }
            },

            withdraw: async (asset, amount) => {
                const token = get().authToken;
                if (!token) return;
                try {
                    const res = await fetch(`${API_BASE}/market/withdraw`, {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${token}` },
                        body: JSON.stringify({ asset, amount })
                    });
                    if (!res.ok) {
                        const errBody = await res.json().catch(() => ({}));
                        console.error('Withdraw failed:', errBody.message || res.statusText);
                        return;
                    }
                    await get().fetchFarmData();
                } catch (e) { console.error('Withdraw error:', e); }
            },

            // Admin Actions
            fetchAdminUsers: async () => {
                const token = get().authToken;
                if (!token) return;
                try {
                    const res = await fetch(`${API_BASE}/admin/users`, {
                        headers: { 'Authorization': `Bearer ${token}` }
                    });
                    if (!res.ok) {
                        const errBody = await res.json().catch(() => ({}));
                        console.error('Failed to fetch admin users:', res.status, errBody.message || errBody.error);
                        return;
                    }
                    const json = await res.json();
                    set({ adminUsers: json.data || [] });
                } catch (e) { console.error('Failed to fetch admin users:', e); }
            },

            fetchPlatformStats: async () => {
                const token = get().authToken;
                if (!token) return;
                try {
                    const res = await fetch(`${API_BASE}/admin/stats`, {
                        headers: { 'Authorization': `Bearer ${token}` }
                    });
                    if (!res.ok) {
                        const errBody = await res.json().catch(() => ({}));
                        console.error('Failed to fetch stats:', res.status, errBody.message || errBody.error);
                        return;
                    }
                    const json = await res.json();
                    set({ platformStats: json.data || null });
                } catch (e) { console.error('Failed to fetch stats:', e); }
            },

            adminTransfer: async (targetWallet: string, itemType: string, amount: string) => {
                const token = get().authToken;
                if (!token) return { success: false, message: 'Not authenticated' };
                try {
                    const res = await fetch(`${API_BASE}/admin/transfer`, {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${token}` },
                        body: JSON.stringify({ target_wallet: targetWallet, item_type: itemType, amount }),
                    });
                    const json = await res.json();
                    if (res.ok) {
                        await get().fetchAdminUsers();
                        return { success: true, message: json.message || 'Transfer berhasil!' };
                    }
                    return { success: false, message: json.message || 'Transfer gagal' };
                } catch (e) {
                    return { success: false, message: 'Network error' };
                }
            },
        }),
        {
            name: 'cash-cow-vault',
            storage: createJSONStorage(() => localStorage),
            partialize: (state) => ({
                authToken: state.authToken,
                userRole: state.userRole,
                userWallet: state.userWallet,
                isAuthenticated: state.isAuthenticated,
                isLoggedIn: state.isLoggedIn,
            }),
        }
    )
);
