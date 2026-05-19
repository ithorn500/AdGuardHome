import React, { useState } from 'react';

import { Link, useLocation } from 'react-router-dom';
import { shallowEqual, useSelector } from 'react-redux';
import { useTranslation } from 'react-i18next';
import classnames from 'classnames';

import Menu from './Menu';

import { Logo } from '../ui/svg/logo';
import './Header.css';
import { RootState } from '../../initialState';

const Header = () => {
    const [isMenuOpen, setIsMenuOpen] = useState(false);
    const { t } = useTranslation();

    const { protectionEnabled, processing, isCoreRunning, processingProfile, name } = useSelector(
        (state: RootState) => state.dashboard,
        shallowEqual,
    );

    const { pathname } = useLocation();

    const toggleMenuOpen = () => {
        setIsMenuOpen((isMenuOpen) => !isMenuOpen);
    };

    const closeMenu = () => {
        setIsMenuOpen(false);
    };

    const badgeClass = classnames('badge dns-status', {
        'badge-success': protectionEnabled,
        'badge-danger': !protectionEnabled,
    });

    return (
        <div className="header">
            <div className="header__container">
                <div className="header__row">
                    <div className="header-toggler d-lg-none ml-lg-0 collapsed" onClick={toggleMenuOpen}>
                        <span className="header-toggler-icon" />
                    </div>

                    <div className="header__column">
                        <div className="d-flex align-items-center">
                            <Link to="/" className="nav-link pl-0 pr-1">
                                <Logo className="header-brand-img" />
                            </Link>
                            <div className="amber-bus-mark" aria-label="Amber Bus connected" title="Amber Bus connected">
                                <svg
                                    className="amber-bus-mark__icon"
                                    viewBox="0 0 24 24"
                                    role="img"
                                    aria-hidden="true"
                                    focusable="false">
                                    <path d="M12 3.4 18.6 7v7.8L12 20.6l-6.6-5.8V7L12 3.4z" />
                                    <path d="M8.4 9.4h7.2M8.4 12h7.2M8.4 14.6h7.2" />
                                </svg>
                                <span className="amber-bus-mark__label">Amber Bus</span>
                            </div>
                            {!processing && isCoreRunning && (
                                <span className={badgeClass}>{t(protectionEnabled ? 'on' : 'off')}</span>
                            )}
                        </div>
                    </div>

                    <Menu pathname={pathname} isMenuOpen={isMenuOpen} closeMenu={closeMenu} />

                    <div className="header__column">
                        <div className="header__right">
                            {!processingProfile && name && (
                                <a href="control/logout" className="btn btn-sm btn-outline-secondary" data-testid="sign_out">
                                    {t('sign_out')}
                                </a>
                            )}
                        </div>
                    </div>
                </div>
            </div>
        </div>
    );
};

export default Header;
