// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

/// @title A bounded cache of recent keys.
contract Cache {
    uint256 public constant CAPACITY = 16;
    bytes32[] private slots;

    event Pushed(address indexed by, bytes32 key);

    error Full();

    function push(bytes32 key) external {
        if (slots.length >= CAPACITY) {
            revert Full();
        }
        slots.push(key);
        emit Pushed(msg.sender, key);
    }

    function size() external view returns (uint256) {
        return slots.length;
    }

    /* Hex and binary literals
       are both valid here. */
    function mask() public pure returns (uint256) {
        return 0xff & 0b1111;
    }
}
